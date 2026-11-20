package requestplugin

import (
	"bytes"
	"context"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/request_plugin_setting"
	"github.com/tidwall/gjson"
)

const moderationPluginKey = "content-moderation"

func moderationBlockStatus(instance *request_plugin_setting.Instance, code string) int {
	if instance.Plugin != moderationPluginKey || code != "moderation.flagged" {
		return 0
	}
	var config struct {
		Status int `json:"blockStatus"`
	}
	if err := common.Unmarshal(instance.Config, &config); err != nil || config.Status < http.StatusBadRequest || config.Status > 599 {
		return 0
	}
	return config.Status
}

func queueModerationIngress(audit *service.ModerationAudit, call Call, instance *request_plugin_setting.Instance, plugin *jsplugin.LoadedPlugin, body []byte) {
	size := len(body)
	if call.form != nil {
		size += len(call.form.raw)
	}
	if size > service.ModerationObservationMaxBytes {
		audit.Skip("oversize")
		return
	}
	call.Body, call.Headers, call.Bound, call.Summary = nil, nil, nil, nil
	if call.form != nil {
		form := *call.form
		form.raw = bytes.Clone(form.raw)
		form.files, form.fields = slices.Clone(form.files), slices.Clone(form.fields)
		call.form = &form
	}
	stableBody := bytes.Clone(body)
	audit.Enqueue(size, func(ctx context.Context) {
		worker := &Plan{}
		_, _, _ = worker.runInstance(ctx, call, instance, plugin, stableBody, nil, audit)
	})
}

func callModerationReply(ctx context.Context, target *replyTarget, tctx jsplugin.TransformContext, stream bool, outputs []jsplugin.ReplyOutput) (jsplugin.ReplyResult, error) {
	audit := service.NewModerationAudit(ctx, target.plugin, target.instance, StageReply, tctx.Model, tctx.User.ID)
	if audit == nil {
		fetcher := service.RequestPluginFetcher(target.plugin, target.instance.Config)
		return target.plugin.CallReply(jsplugin.WithFetcher(ctx, fetcher), tctx, stream, outputs)
	}
	if !audit.Sampled() || !gjson.GetBytes(target.instance.Config, "replies").Bool() {
		return jsplugin.ReplyResult{Action: "pass"}, nil
	}
	if !audit.Observes() {
		return executeModerationReply(ctx, target.plugin, target.instance, audit, tctx, stream, outputs)
	}
	if target.observing == nil {
		target.observing = make(map[int]string)
	}
	var ready []jsplugin.ReplyOutput
	for _, output := range outputs {
		buffered := target.observing[output.Index]
		target.observationBytes += len(output.Text)
		if target.observationBytes > service.ModerationObservationMaxBytes {
			target.observing = nil
			audit.Skip("oversize")
			return jsplugin.ReplyResult{Action: "pass"}, nil
		}
		text := buffered + output.Text
		if output.Final || utf8.RuneCountInString(text) >= 1000 {
			target.observationBytes -= len(text)
			delete(target.observing, output.Index)
			if strings.TrimSpace(text) != "" {
				ready = append(ready, jsplugin.ReplyOutput{Index: output.Index, Text: strings.Clone(text), Final: true})
			}
		} else {
			target.observing[output.Index] = strings.Clone(text)
		}
	}
	if len(ready) > 0 {
		size := 0
		for _, output := range ready {
			size += len(output.Text)
		}
		plugin, instance := target.plugin, target.instance
		audit.Enqueue(size, func(workerCtx context.Context) {
			_, _ = executeModerationReply(workerCtx, plugin, instance, audit, tctx, stream, ready)
		})
	}
	return jsplugin.ReplyResult{Action: "continue"}, nil
}

func executeModerationReply(ctx context.Context, plugin *jsplugin.LoadedPlugin, instance *request_plugin_setting.Instance, audit *service.ModerationAudit,
	tctx jsplugin.TransformContext, stream bool, outputs []jsplugin.ReplyOutput) (jsplugin.ReplyResult, error) {
	audit.ConfigureCapture(plugin, instance)
	fetcher := audit.Fetcher(service.RequestPluginFetcher(plugin, instance.Config))
	result, err := plugin.CallReply(jsplugin.WithFetcher(ctx, fetcher), tctx, stream, outputs)
	category := ""
	if len(result.Findings) > 0 {
		category = result.Findings[0].Category
	}
	audit.Record(result.Action, result.Code, category, err != nil, len(result.Notice) > 0)
	return result, err
}
