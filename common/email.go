package common

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/smtp"
	"slices"
	"strings"
	"time"
)

func generateMessageID(from string) (string, error) {
	split := strings.Split(from, "@")
	if len(split) < 2 {
		return "", fmt.Errorf("invalid SMTP account")
	}
	domain := split[1]
	return fmt.Sprintf("<%d.%s@%s>", time.Now().UnixNano(), GetRandomString(12), domain), nil
}

func shouldUseSMTPLoginAuth() bool {
	if SMTPForceAuthLogin {
		return true
	}
	return isOutlookServer(SMTPAccount) || slices.Contains(EmailLoginAuthServerList, SMTPServer)
}

func getSMTPAuth() smtp.Auth {
	return AutoSMTPAuth(SMTPAccount, SMTPToken)
}

func shouldAuthenticateSMTP() bool {
	return SMTPAccount != "" && SMTPToken != ""
}

func smtpTLSConfig() *tls.Config {
	return &tls.Config{
		ServerName:         SMTPServer,
		InsecureSkipVerify: SMTPInsecureSkipVerify, // #nosec G402 -- admin-controlled SMTP compatibility option.
	}
}

func newSMTPClient(addr string) (*smtp.Client, error) {
	return newSMTPClientContext(context.Background(), addr)
}

func newSMTPClientContext(ctx context.Context, addr string) (*smtp.Client, error) {
	implicitTLS := SMTPSSLEnabled || (SMTPPort == 465 && !SMTPStartTLSEnabled)
	var conn net.Conn
	var err error
	if implicitTLS {
		dialer := &tls.Dialer{Config: smtpTLSConfig()}
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	} else {
		conn, err = (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return nil, err
	}
	if deadline, hasDeadline := ctx.Deadline(); hasDeadline {
		if err := conn.SetDeadline(deadline); err != nil {
			_ = conn.Close()
			return nil, err
		}
	}
	host := SMTPServer
	if !implicitTLS {
		host, _, _ = net.SplitHostPort(addr)
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}

	if !implicitTLS && SMTPStartTLSEnabled {
		startTLSSupported, _ := client.Extension("STARTTLS")
		if !startTLSSupported {
			_ = client.Close()
			return nil, fmt.Errorf("SMTP server does not support STARTTLS")
		}
		if err := client.StartTLS(smtpTLSConfig()); err != nil {
			_ = client.Close()
			return nil, err
		}
	}

	return client, nil
}

func SendEmail(subject string, receiver string, content string) error {
	return SendEmailContext(context.Background(), subject, receiver, content)
}

func SendEmailContext(ctx context.Context, subject string, receiver string, content string) error {
	OptionMapRWMutex.RLock()
	provider := OptionMap["EmailProvider"]
	resendKey := OptionMap["ResendApiKey"]
	resendFrom := OptionMap["ResendFrom"]
	OptionMapRWMutex.RUnlock()
	// One selector controls delivery. Never fall back to a second provider:
	// a failed response does not prove that the first provider did not send.
	switch provider {
	case "resend":
		return sendResendEmailContext(ctx, resendKey, resendFrom, subject, receiver, content)
	case "", "smtp": // Preserve existing installations without a selector.
	default:
		return NewMessage("Email sending method must be SMTP or Resend")
	}
	from := SMTPFrom
	if from == "" {
		from = SMTPAccount
	}
	id, err2 := generateMessageID(from)
	if err2 != nil {
		return err2
	}
	if SMTPServer == "" && SMTPAccount == "" {
		return NewMessage("SMTP server is not configured")
	}
	encodedSubject := fmt.Sprintf("=?UTF-8?B?%s?=", base64.StdEncoding.EncodeToString([]byte(subject)))
	mail := []byte(fmt.Sprintf("To: %s\r\n"+
		"From: %s <%s>\r\n"+
		"Subject: %s\r\n"+
		"Date: %s\r\n"+
		"Message-ID: %s\r\n"+ // 添加 Message-ID 头
		"Content-Type: text/html; charset=UTF-8\r\n\r\n%s\r\n",
		receiver, SystemName, from, encodedSubject, time.Now().Format(time.RFC1123Z), id, content))
	auth := getSMTPAuth()
	addr := fmt.Sprintf("%s:%d", SMTPServer, SMTPPort)
	to := strings.Split(receiver, ";")
	var err error
	client, err := newSMTPClientContext(ctx, addr)
	if err != nil {
		return err
	}
	defer client.Close()
	if shouldAuthenticateSMTP() {
		if err = client.Auth(auth); err != nil {
			return err
		}
	}
	if err = client.Mail(from); err != nil {
		return err
	}
	for _, receiver := range to {
		if err = client.Rcpt(receiver); err != nil {
			return err
		}
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	_, err = w.Write(mail)
	if err != nil {
		return err
	}
	err = w.Close()
	if err != nil {
		return err
	}
	err = client.Quit()
	if err != nil {
		SysError(LogText("failed to send email to %s: %v", receiver, err))
	}
	return err
}
