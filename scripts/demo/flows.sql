-- Background traffic for AS64500 "Example Networks" (fictional operator).
--
-- One row per (slot, link, direction, remote AS, k). Placeholders, replaced by
-- seed.sh / tick.sh with sed:
--   {T0}    end of the generated range (a DateTime expression)
--   {N}     number of slots
--   {SPAN}  seconds per slot (60 for the backfill, 30 for the live tick)
--   {K}     rows per (link, direction, AS) per slot: more rows = more flows/s,
--           same bit rate
--
-- Local side: documentation prefixes only (192.0.2.0/24, 198.51.100.0/24,
-- 203.0.113.0/24, 2001:db8::/32). Remote side: RFC 2544 benchmarking space
-- (198.18.0.0/15) and the IPv6 documentation prefix 3fff::/20, so no address
-- belongs to anyone real. Remote AS numbers carry well-known network names with
-- invented volumes.
INSERT INTO asstats.flows_raw
  (timestamp, router_ip, link_tag, src_ip, dst_ip, ip_version, src_as, dst_as,
   src_prefix, dst_prefix, protocol, src_port, dst_port, tcp_flags, bytes,
   packets, sampling_rate, direction, flow_type)
WITH
  ['TRANSIT-A', 'TRANSIT-B', 'IX-PAR', 'PNI-CDN', 'IX-AMS'] AS tags,
  ['::ffff:192.0.2.252', '::ffff:192.0.2.252', '::ffff:192.0.2.253', '::ffff:192.0.2.253', '::ffff:192.0.2.253'] AS routers,
  -- Mb/s at the evening peak, per link
  [3700.0, 740.0, 2300.0, 3100.0, 250.0] AS peak_in,
  [1500.0, 300.0, 900.0, 260.0, 170.0] AS peak_out,
  [13335, 15169, 20940, 2906, 16509, 8075, 32934, 16276, 24940, 3215, 12322, 3320, 1299,
   64512, 65001, 4200000001, 4200000123, 12876] AS ases,
  -- share weights per link (rows) and AS (columns); the last four numeric
  -- columns before Scaleway are private ASNs (~30 %)
  [[6, 5, 3, 2, 8, 6, 3, 6, 5, 7, 6, 5, 8, 10, 8, 6, 4, 4],
   [3, 3, 1, 1, 5, 4, 2, 8, 9, 6, 5, 4, 10, 12, 9, 8, 5, 4],
   [8, 6, 2, 1, 3, 6, 6, 10, 3, 12, 12, 2, 0, 9, 8, 7, 5, 5],
   [3, 7, 12, 26, 0, 0, 0, 0, 0, 0, 0, 0, 0, 6, 4, 3, 2, 0],
   [5, 3, 3, 2, 4, 3, 4, 4, 8, 1, 1, 6, 3, 9, 8, 7, 5, 3]] AS wts,
  -- hourly shape (UTC), evening peak
  [0.42, 0.34, 0.29, 0.26, 0.25, 0.27, 0.33, 0.43, 0.55, 0.63, 0.68, 0.71,
   0.74, 0.75, 0.74, 0.75, 0.78, 0.82, 0.88, 0.95, 1.0, 0.99, 0.86, 0.62] AS hourly,
  [443, 443, 443, 443, 80, 80, 25, 993, 22, 8443, 3389, 5222] AS tcp_ports,
  [53, 443, 443, 123, 3478, 51820, 4500, 443] AS udp_ports,
  18 AS NAS,
  NAS * 2 * 5 * {K} AS per_slot,
  number % per_slot AS combo,
  intDiv(number, per_slot) AS slot,
  intDiv(combo, {K}) AS c,
  combo % {K} AS k,
  c % NAS AS as_i,
  intDiv(c, NAS) % 2 AS dir_i,
  intDiv(c, NAS * 2) % 5 AS link_i,
  cityHash64(number, {T0}) AS hsh,
  cityHash64(hsh, 7) AS h2,
  cityHash64(hsh, 11) AS h3,
  {T0} - toIntervalSecond({SPAN} * ({N} - slot)) + toIntervalSecond(hsh % {SPAN}) AS ts,
  toFloat64(now() - ts) / 86400 AS age,
  toHour(ts) AS hr,
  hourly[hr + 1] + (hourly[(hr + 1) % 24 + 1] - hourly[hr + 1]) * toMinute(ts) / 60 AS shape,
  if(toDayOfWeek(ts) >= 6, 0.85, 1.0) AS weekend,
  0.95 + 0.10 * (cityHash64(toStartOfFiveMinutes(ts), link_i, dir_i) % 1000) / 1000 AS noise,
  -- Transit B grows ~16 % over two weeks: it is the link to watch
  if(link_i = 1, 0.84 + 0.16 * greatest(0, 1 - age / 14), 1.0 - 0.03 * least(age, 14) / 14) AS growth,
  if(dir_i = 0, peak_in[link_i + 1], peak_out[link_i + 1]) * shape * weekend * noise * growth AS link_mbps,
  multiIf(
    as_i = 8 AND age < 1, 2.4,                      -- Hetzner: nightly backups moved in
    as_i = 11 AND age < 1.05, 0.0,                  -- Deutsche Telekom: gone since yesterday
    as_i = 12 AND age < 1, 0.5,                     -- Arelion: half of yesterday
    as_i = 17, if(age < 0.9, 1.0, 0.0),             -- Scaleway: new today
    as_i = 3, 0.75 + 0.25 * greatest(0, 1 - age / 14), -- Netflix: slow growth
    1.0) AS as_mult,
  wts[link_i + 1][as_i + 1] AS w,
  arraySum(wts[link_i + 1]) AS wsum,
  (0.85 + 0.3 * (h3 % 1000) / 1000) AS jitter,
  -- local host: 170 IPv4 + 30 IPv6 addresses, a few busy ones and a long
  -- tail. One row in four is IPv6, so the address-family split is steady.
  k % 4 = 0 AS v6,
  pow((h2 % 100000) / 100000, 1.6) AS u,
  if(v6, 170 + (toUInt32(floor(30 * u)) * 7) % 30, (toUInt32(floor(170 * u)) * 73) % 170) AS hi,
  multiIf(hi < 70, concat('::ffff:192.0.2.', toString(hi + 10)),
          hi < 130, concat('::ffff:198.51.100.', toString(hi - 70 + 20)),
          hi < 170, concat('::ffff:203.0.113.', toString(hi - 130 + 5)),
          concat('2001:db8:', toString(1 + hi % 3), '::', lower(hex(toUInt8(hi))))) AS local_ip,
  multiIf(hi < 70, '192.0.2.0/24', hi < 130, '198.51.100.0/24', hi < 170, '203.0.113.0/24',
          concat('2001:db8:', toString(1 + hi % 3), '::/48')) AS local_pfx,
  if(v6,
     concat('3fff:', lower(hex(toUInt8(as_i + 16))), '::', lower(hex(toUInt16(h3 % 40 + 1)))),
     concat('::ffff:198.', toString(18 + intDiv(as_i, 9)), '.', toString((as_i % 9) * 28 + h3 % 4), '.', toString(h3 % 10 * 13 + 7))) AS remote_ip,
  if(v6,
     concat('3fff:', lower(hex(toUInt8(as_i + 16))), '::/32'),
     concat('198.', toString(18 + intDiv(as_i, 9)), '.', toString((as_i % 9) * 28 + h3 % 4), '.0/24')) AS remote_pfx,
  hsh % 1000 AS pr,
  multiIf(pr < 860, 6, pr < 995, 17, if(v6, 58, 1)) AS proto,
  multiIf(proto = 6, tcp_ports[h3 % 12 + 1], proto = 17, udp_ports[h3 % 8 + 1], 0) AS svc_port,
  toUInt16(if(proto IN (1, 58), 0, 32768 + h2 % 2000)) AS eph_port,
  multiIf(proto = 6, 1150, proto = 17, 700, 98) AS pkt_size,
  toUInt64(link_mbps * 1e6 * {SPAN} / 8 * w * as_mult / wsum / {K} * jitter * if(local_ip = '::ffff:192.0.2.40', 0.08, 1.0)) AS b
SELECT
  ts,
  toIPv6(routers[link_i + 1]),
  tags[link_i + 1],
  toIPv6(if(dir_i = 0, remote_ip, local_ip)),
  toIPv6(if(dir_i = 0, local_ip, remote_ip)),
  if(v6, 6, 4),
  if(dir_i = 0, ases[as_i + 1], 0),
  if(dir_i = 0, 0, ases[as_i + 1]),
  if(dir_i = 0, remote_pfx, local_pfx),
  if(dir_i = 0, local_pfx, remote_pfx),
  proto,
  if(dir_i = 0, eph_port, svc_port),
  if(dir_i = 0, svc_port, eph_port),
  if(proto = 6, 24, 0),
  b,
  intDiv(b, pkt_size) + 1,
  1,
  if(dir_i = 0, 'in', 'out'),
  if(link_i >= 2, 'sflow', 'netflow')
FROM numbers({N} * per_slot)
WHERE b > 0
