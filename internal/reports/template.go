package reports

// reportTemplate is the HTML body for scheduled reports. It uses inline styles
// only (email clients strip <style>/external CSS) and a dark-on-light palette
// for broad client compatibility. Sections render only when present in
// .Sections. Rendered via html/template, so all interpolated values are escaped.
const reportTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>{{.ScheduleName}}</title>
</head>
<body style="margin:0;padding:0;background:#f4f5f7;font-family:-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;color:#1a1d21;">
<div style="max-width:720px;margin:0 auto;padding:24px;">
  <div style="background:#0f1419;border-radius:8px 8px 0 0;padding:20px 24px;">
    <div style="color:#e6e8eb;font-size:18px;font-weight:600;">AS-Stats — {{.ScheduleName}}</div>
    <div style="color:#8b949e;font-size:13px;margin-top:4px;">
      {{.Frequency}} report · {{fmtTime .From}} → {{fmtTime .To}}
    </div>
  </div>
  <div style="background:#ffffff;border-radius:0 0 8px 8px;padding:8px 24px 24px;border:1px solid #e1e4e8;border-top:none;">

  {{if and .Sections.overview .Overview}}
    <h2 style="font-size:15px;margin:20px 0 8px;color:#1a1d21;">Overview</h2>
    <table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="border-collapse:collapse;font-size:13px;">
      <tr>
        <td style="padding:8px 0;color:#57606a;">Total in</td>
        <td style="padding:8px 0;text-align:right;font-variant-numeric:tabular-nums;">{{humanBytes .Overview.TotalBytesIn}}</td>
      </tr>
      <tr>
        <td style="padding:8px 0;color:#57606a;border-top:1px solid #eaecef;">Total out</td>
        <td style="padding:8px 0;text-align:right;font-variant-numeric:tabular-nums;border-top:1px solid #eaecef;">{{humanBytes .Overview.TotalBytesOut}}</td>
      </tr>
      <tr>
        <td style="padding:8px 0;color:#57606a;border-top:1px solid #eaecef;">Total flows</td>
        <td style="padding:8px 0;text-align:right;font-variant-numeric:tabular-nums;border-top:1px solid #eaecef;">{{.Overview.TotalFlows}}</td>
      </tr>
      <tr>
        <td style="padding:8px 0;color:#57606a;border-top:1px solid #eaecef;">Active ASes</td>
        <td style="padding:8px 0;text-align:right;font-variant-numeric:tabular-nums;border-top:1px solid #eaecef;">{{.Overview.ActiveASCount}}</td>
      </tr>
    </table>
  {{end}}

  {{if and .Sections.top_as .TopAS}}
    <h2 style="font-size:15px;margin:24px 0 8px;color:#1a1d21;">Top AS</h2>
    <table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="border-collapse:collapse;font-size:13px;">
      <tr style="color:#57606a;text-align:left;">
        <th style="padding:6px 8px;font-weight:600;border-bottom:2px solid #eaecef;">AS</th>
        <th style="padding:6px 8px;font-weight:600;border-bottom:2px solid #eaecef;">Name</th>
        <th style="padding:6px 8px;font-weight:600;text-align:right;border-bottom:2px solid #eaecef;">Bytes</th>
        <th style="padding:6px 8px;font-weight:600;text-align:right;border-bottom:2px solid #eaecef;">%</th>
      </tr>
      {{range .TopAS}}
      <tr>
        <td style="padding:6px 8px;border-bottom:1px solid #f0f2f4;font-variant-numeric:tabular-nums;">AS{{.ASNumber}}</td>
        <td style="padding:6px 8px;border-bottom:1px solid #f0f2f4;">{{.ASName}}{{if .Country}} ({{.Country}}){{end}}</td>
        <td style="padding:6px 8px;text-align:right;border-bottom:1px solid #f0f2f4;font-variant-numeric:tabular-nums;">{{humanBytes .Bytes}}</td>
        <td style="padding:6px 8px;text-align:right;border-bottom:1px solid #f0f2f4;font-variant-numeric:tabular-nums;">{{pct .Percent}}</td>
      </tr>
      {{end}}
    </table>
  {{end}}

  {{if and .Sections.top_country .TopCountry}}
    <h2 style="font-size:15px;margin:24px 0 8px;color:#1a1d21;">Top Countries</h2>
    <table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="border-collapse:collapse;font-size:13px;">
      <tr style="color:#57606a;text-align:left;">
        <th style="padding:6px 8px;font-weight:600;border-bottom:2px solid #eaecef;">Country</th>
        <th style="padding:6px 8px;font-weight:600;text-align:right;border-bottom:2px solid #eaecef;">Bytes</th>
        <th style="padding:6px 8px;font-weight:600;text-align:right;border-bottom:2px solid #eaecef;">%</th>
      </tr>
      {{range .TopCountry}}
      <tr>
        <td style="padding:6px 8px;border-bottom:1px solid #f0f2f4;">{{.Country}}{{if .Name}} · {{.Name}}{{end}}</td>
        <td style="padding:6px 8px;text-align:right;border-bottom:1px solid #f0f2f4;font-variant-numeric:tabular-nums;">{{humanBytes .Bytes}}</td>
        <td style="padding:6px 8px;text-align:right;border-bottom:1px solid #f0f2f4;font-variant-numeric:tabular-nums;">{{pct .Percent}}</td>
      </tr>
      {{end}}
    </table>
  {{end}}

  {{if and .Sections.capacity .Capacity}}
    <h2 style="font-size:15px;margin:24px 0 8px;color:#1a1d21;">Link Capacity</h2>
    <table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="border-collapse:collapse;font-size:13px;">
      <tr style="color:#57606a;text-align:left;">
        <th style="padding:6px 8px;font-weight:600;border-bottom:2px solid #eaecef;">Link</th>
        <th style="padding:6px 8px;font-weight:600;text-align:right;border-bottom:2px solid #eaecef;">Capacity</th>
        <th style="padding:6px 8px;font-weight:600;text-align:right;border-bottom:2px solid #eaecef;">p95</th>
        <th style="padding:6px 8px;font-weight:600;text-align:right;border-bottom:2px solid #eaecef;">Util</th>
      </tr>
      {{range .Capacity}}
      <tr>
        <td style="padding:6px 8px;border-bottom:1px solid #f0f2f4;">{{.Tag}}</td>
        <td style="padding:6px 8px;text-align:right;border-bottom:1px solid #f0f2f4;font-variant-numeric:tabular-nums;">{{if .CapacityMbps}}{{.CapacityMbps}} Mbps{{else}}—{{end}}</td>
        <td style="padding:6px 8px;text-align:right;border-bottom:1px solid #f0f2f4;font-variant-numeric:tabular-nums;">{{humanBps .P95Bps}}</td>
        <td style="padding:6px 8px;text-align:right;border-bottom:1px solid #f0f2f4;font-variant-numeric:tabular-nums;">{{utilPct .UtilizationPct}}</td>
      </tr>
      {{end}}
    </table>
  {{end}}

  {{if .Sections.alerts}}
    <h2 style="font-size:15px;margin:24px 0 8px;color:#1a1d21;">Active Alerts ({{.AlertTotal}})</h2>
    {{if .AlertRows}}
    <table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="border-collapse:collapse;font-size:13px;">
      <tr style="color:#57606a;text-align:left;">
        <th style="padding:6px 8px;font-weight:600;border-bottom:2px solid #eaecef;">Severity</th>
        <th style="padding:6px 8px;font-weight:600;text-align:right;border-bottom:2px solid #eaecef;">Count</th>
      </tr>
      {{range .AlertRows}}
      <tr>
        <td style="padding:6px 8px;border-bottom:1px solid #f0f2f4;text-transform:capitalize;">{{.Severity}}</td>
        <td style="padding:6px 8px;text-align:right;border-bottom:1px solid #f0f2f4;font-variant-numeric:tabular-nums;">{{.Count}}</td>
      </tr>
      {{end}}
    </table>
    {{else}}
    <p style="font-size:13px;color:#57606a;">No active alerts.</p>
    {{end}}
  {{end}}

    <div style="margin-top:28px;padding-top:16px;border-top:1px solid #eaecef;color:#8b949e;font-size:11px;">
      Generated {{fmtTime .GeneratedAt}} by AS-Stats. CSV data attached.
    </div>
  </div>
</div>
</body>
</html>`
