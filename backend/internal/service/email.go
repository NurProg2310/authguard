package service

import (
	"fmt"
	"html"
)

func resetEmailHTML(name, link string, minutes int) string {
	n, l := html.EscapeString(name), html.EscapeString(link)
	return fmt.Sprintf(`<!doctype html>
<html><body style="margin:0;padding:0;background:#fafafa;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;color:#18181b">
<table role="presentation" width="100%%" cellpadding="0" cellspacing="0"><tr><td align="center" style="padding:48px 16px">
<table role="presentation" width="100%%" style="max-width:480px;background:#ffffff;border:1px solid #e4e4e7;border-radius:12px" cellpadding="0" cellspacing="0">
<tr><td style="padding:32px">
  <div style="font-weight:600;font-size:15px;letter-spacing:-0.01em;margin-bottom:24px">AuthGuard</div>
  <h1 style="font-size:20px;font-weight:600;letter-spacing:-0.02em;margin:0 0 12px">Reset your password</h1>
  <p style="font-size:14px;line-height:22px;color:#52525b;margin:0 0 24px">Hi %s, we received a request to reset your password. This link expires in %d minutes and can be used once.</p>
  <a href="%s" style="display:inline-block;background:#18181b;color:#fafafa;text-decoration:none;font-size:14px;font-weight:500;padding:10px 18px;border-radius:8px">Reset password</a>
  <p style="font-size:12px;line-height:20px;color:#71717a;margin:24px 0 0">Resetting your password signs you out everywhere. If you didn't request this, you can safely ignore this email.</p>
</td></tr></table>
</td></tr></table></body></html>`, n, minutes, l)
}
