package model

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestContentModerationAuditDatabaseMatrix(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			var driver gorm.Dialector
			switch engine {
			case "sqlite":
				driver = sqlite.Open(":memory:")
			case "mysql":
				dsn := os.Getenv("TEST_MYSQL_DSN")
				if dsn == "" {
					t.Skip("TEST_MYSQL_DSN not configured")
				}
				driver = mysql.Open(dsn)
			case "postgres":
				dsn := os.Getenv("TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("TEST_POSTGRES_DSN not configured")
				}
				driver = postgres.Open(dsn)
			}
			db, err := gorm.Open(driver, &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: "mod_audit_test_"}})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			previousDB, previousType := DB, common.MainDatabaseType()
			DB = db
			common.SetMainDatabaseType(common.DatabaseType(engine))
			useUserCacheMiniRedis(t)
			models := []any{&ContentModerationAudit{}, &ContentModerationIncident{}, &ContentModerationSuspension{}, &User{}}
			t.Cleanup(func() {
				assert.NoError(t, db.Migrator().DropTable(models...))
				DB = previousDB
				common.SetMainDatabaseType(previousType)
				assert.NoError(t, sqlDB.Close())
			})
			query := "SELECT version()"
			if engine == "sqlite" {
				query = "SELECT sqlite_version()"
			}
			var version string
			require.NoError(t, db.Raw(query).Scan(&version).Error)
			t.Logf("database version: %s", version)
			require.NoError(t, db.AutoMigrate(&User{}))
			user := User{Username: "moderation-fixture", Email: "recipient@example.invalid", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AuthVersion: 1, AffCode: "ma1"}
			admin := User{Username: "moderation-admin", Role: common.RoleAdminUser, Status: common.UserStatusEnabled, AuthVersion: 1, AffCode: "ma2"}
			require.NoError(t, db.Create(&user).Error)
			require.NoError(t, db.Create(&admin).Error)
			for range 2 {
				require.NoError(t, db.AutoMigrate(models...))
			}
			now := time.Now().Unix()
			ctx := t.Context()
			store := func(key, request, decision, mode string, userID int, created int64) ContentModerationAudit {
				event := ContentModerationAudit{EventKey: key, RequestID: request, UserID: userID, InstanceID: "fixture", InstanceName: "Fixture", Stage: "ingress", Engine: "openai", Category: "violence", Decision: decision, Mode: mode, CreatedAt: created, ExpiresAt: created + 86400, RiskHash: "fixture-hash"}
				require.NoError(t, StoreContentModerationAudit(ctx, &event))
				return event
			}
			first := store("first", "request-one", ModerationAuditHit, "observe", user.Id, now-5)
			actions, err := ApplyContentModerationIncident(ctx, &first, true, true, 2, 24)
			require.NoError(t, err)
			assert.True(t, actions.NotifyHit)
			assert.Equal(t, user.Email, actions.Email)
			assert.False(t, actions.Disabled)

			repeated := store("repeated", "request-one", ModerationAuditHit, "observe", user.Id, now-4)
			actions, err = ApplyContentModerationIncident(ctx, &repeated, true, true, 2, 24)
			require.NoError(t, err)
			assert.Zero(t, actions)
			trusted := store("trusted", "request-trusted", ModerationAuditHit, "only_record", user.Id, now-3)
			actions, err = ApplyContentModerationIncident(ctx, &trusted, true, true, 1, 24)
			require.NoError(t, err)
			assert.Zero(t, actions)

			second := store("second", "request-two", ModerationAuditBlock, "enforce", user.Id, now-2)
			actions, err = ApplyContentModerationIncident(ctx, &second, false, true, 2, 24)
			require.NoError(t, err)
			assert.True(t, actions.Disabled)
			assert.True(t, actions.NotifyBan)
			assert.False(t, actions.NotifyHit)
			var read User
			require.NoError(t, db.First(&read, user.Id).Error)
			assert.Equal(t, common.UserStatusDisabled, read.Status)
			assert.Greater(t, read.AuthVersion, user.AuthVersion)
			assert.Equal(t, user.Email, read.Email)
			require.ErrorIs(t, ReleaseContentModerationSuspension(ctx, user.Id, first.ID), ErrModerationSuspensionChanged)
			require.NoError(t, ReleaseContentModerationSuspension(ctx, user.Id, second.ID))
			require.ErrorIs(t, ReleaseContentModerationSuspension(ctx, user.Id, second.ID), ErrModerationSuspensionChanged)
			var released User
			require.NoError(t, db.First(&released, user.Id).Error)
			assert.Equal(t, common.UserStatusEnabled, released.Status)
			assert.Greater(t, released.AuthVersion, read.AuthVersion)

			adminHit := store("admin", "admin-request", ModerationAuditBlock, "enforce", admin.Id, now)
			actions, err = ApplyContentModerationIncident(ctx, &adminHit, false, true, 1, 24)
			require.NoError(t, err)
			assert.False(t, actions.Disabled)
			releaseAudit, err := ContentModerationSuspensionAudit(ctx, second.ID)
			require.NoError(t, err)
			next := store("manual", "manual-request", ModerationAuditBlock, "enforce", user.Id, releaseAudit.ReleasedAt+1)
			actions, err = ApplyContentModerationIncident(ctx, &next, false, true, 1, 24)
			require.NoError(t, err)
			require.True(t, actions.Disabled)
			changed, err := SetUserStatusManually(user.Id, common.UserStatusDisabled, common.RoleRootUser)
			require.NoError(t, err)
			assert.False(t, changed)
			err = ReleaseContentModerationSuspension(ctx, user.Id, next.ID)
			require.True(t, errors.Is(err, ErrModerationSuspensionChanged))

			expired := store("expired", "expired-request", ModerationAuditPass, "enforce", user.Id, now-86401)
			category, hit, err := ContentModerationHistory(ctx, "fixture-hash", now)
			require.NoError(t, err)
			assert.True(t, hit)
			assert.Equal(t, "violence", category)
			count, err := DeleteExpiredContentModerationAudits(ctx, now, 50)
			require.NoError(t, err)
			assert.EqualValues(t, 1, count)
			rows, more, err := ListContentModerationAudits(ctx, ContentModerationAuditFilter{UserID: user.Id, Decision: ModerationAuditBlock, Limit: 1})
			require.NoError(t, err)
			require.Len(t, rows, 1)
			assert.True(t, more)
			assert.Equal(t, next.ID, rows[0].ID)
			rows, more, err = ListContentModerationAudits(ctx, ContentModerationAuditFilter{UserID: user.Id, BeforeID: rows[0].ID, Decision: ModerationAuditBlock, Limit: 10})
			require.NoError(t, err)
			require.Len(t, rows, 1)
			assert.False(t, more)
			assert.Equal(t, second.ID, rows[0].ID)
			require.ErrorIs(t, db.First(&ContentModerationAudit{}, expired.ID).Error, gorm.ErrRecordNotFound)
		})
	}
}
