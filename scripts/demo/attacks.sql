-- Ongoing events for Live Threats and the alert list. Same placeholders as
-- flows.sql ({T0}, {N}, {SPAN}). Each event is active while "minutes ago" is
-- at most its start; the live tick (N = 1) always continues them.
--
--   SYN flood       -> 203.0.113.10  tcp/443, ~350k pps of 60-byte SYNs, started 14 min ago
--   NTP reflection  -> 198.51.100.25 udp/123 from ~2,500 reflectors/min, ~1.3 Gb/s, 9 min ago
--   port scan       <- 198.51.100.77 (a customer VM) 600 ports/min, 22 min ago
--   backup burst    -> 192.0.2.40    from Hetzner, ~260 Mb/s against a usual few Mb/s, 31 min ago

-- SYN flood
INSERT INTO asstats.flows_raw
  (timestamp, router_ip, link_tag, src_ip, dst_ip, ip_version, src_as, dst_as, src_prefix, dst_prefix,
   protocol, src_port, dst_port, tcp_flags, bytes, packets, sampling_rate, direction)
WITH 300 AS R, intDiv(number, R) AS slot, {N} - slot AS m_ago, cityHash64(number, {T0}, 1) AS hsh,
  [13335, 16509, 24940, 16276, 3215, 12322, 1299, 8075] AS spoof_as,
  toUInt64(350000 * {SPAN} / R) AS pk
SELECT {T0} - toIntervalSecond({SPAN} * m_ago) + toIntervalSecond(hsh % {SPAN}),
  toIPv6('::ffff:192.0.2.252'), 'TRANSIT-A',
  toIPv6(concat('::ffff:198.', toString(18 + hsh % 2), '.', toString(intDiv(hsh, 3) % 256), '.', toString(intDiv(hsh, 7) % 254 + 1))),
  toIPv6('::ffff:203.0.113.10'), 4, spoof_as[hsh % 8 + 1], 0, '', '203.0.113.0/24',
  6, toUInt16(1024 + intDiv(hsh, 11) % 64000), 443, 2, pk * 60, pk, 1, 'in'
FROM numbers({N} * R) WHERE m_ago <= 14;

-- NTP reflection
INSERT INTO asstats.flows_raw
  (timestamp, router_ip, link_tag, src_ip, dst_ip, ip_version, src_as, dst_as, src_prefix, dst_prefix,
   protocol, src_port, dst_port, tcp_flags, bytes, packets, sampling_rate, direction)
WITH 2500 AS R, intDiv(number, R) AS slot, {N} - slot AS m_ago, cityHash64(number, {T0}, 2) AS hsh,
  [3215, 12322, 20940, 1299, 16276, 24940, 64512, 65001] AS refl_as,
  toUInt64(1.3e9 * {SPAN} / 8 / R / 468) AS pk
SELECT {T0} - toIntervalSecond({SPAN} * m_ago) + toIntervalSecond(hsh % {SPAN}),
  toIPv6(if(hsh % 2 = 0, '::ffff:192.0.2.252', '::ffff:192.0.2.253')), if(hsh % 2 = 0, 'TRANSIT-A', 'IX-PAR'),
  toIPv6(concat('::ffff:198.', toString(18 + hsh % 2), '.', toString(intDiv(hsh, 3) % 256), '.', toString(intDiv(hsh, 7) % 254 + 1))),
  toIPv6('::ffff:198.51.100.25'), 4, refl_as[hsh % 8 + 1], 0, '', '198.51.100.0/24',
  17, 123, toUInt16(1024 + intDiv(hsh, 11) % 64000), 0, pk * 468, pk, 1, 'in'
FROM numbers({N} * R) WHERE m_ago <= 9;

-- Outbound port scan from a customer VM
INSERT INTO asstats.flows_raw
  (timestamp, router_ip, link_tag, src_ip, dst_ip, ip_version, src_as, dst_as, src_prefix, dst_prefix,
   protocol, src_port, dst_port, tcp_flags, bytes, packets, sampling_rate, direction)
WITH 600 AS R, intDiv(number, R) AS slot, {N} - slot AS m_ago, cityHash64(number, {T0}, 3) AS hsh,
  toUInt64(3000 * {SPAN} / R) AS pk
SELECT {T0} - toIntervalSecond({SPAN} * m_ago) + toIntervalSecond(hsh % {SPAN}),
  toIPv6('::ffff:192.0.2.252'), 'TRANSIT-B',
  toIPv6('::ffff:198.51.100.77'),
  toIPv6(concat('::ffff:198.', toString(18 + hsh % 2), '.', toString(intDiv(hsh, 3) % 256), '.', toString(intDiv(hsh, 7) % 254 + 1))),
  4, 0, [16509, 8075, 15169, 16276, 24940, 3215][hsh % 6 + 1], '198.51.100.0/24', '',
  6, toUInt16(40000 + hsh % 20000), toUInt16(1 + (number % R + m_ago * 613) % 10000), 2, pk * 64, pk, 1, 'out'
FROM numbers({N} * R) WHERE m_ago <= 22;

-- Backup burst from Hetzner to 192.0.2.40
INSERT INTO asstats.flows_raw
  (timestamp, router_ip, link_tag, src_ip, dst_ip, ip_version, src_as, dst_as, src_prefix, dst_prefix,
   protocol, src_port, dst_port, tcp_flags, bytes, packets, sampling_rate, direction)
WITH 20 AS R, intDiv(number, R) AS slot, {N} - slot AS m_ago, cityHash64(number, {T0}, 4) AS hsh,
  toUInt64(260e6 * {SPAN} / 8 / R) AS b
SELECT {T0} - toIntervalSecond({SPAN} * m_ago) + toIntervalSecond(hsh % {SPAN}),
  toIPv6('::ffff:192.0.2.253'), 'IX-AMS',
  toIPv6(concat('::ffff:198.18.', toString(224 + hsh % 28), '.', toString(intDiv(hsh, 7) % 254 + 1))),
  toIPv6('::ffff:192.0.2.40'), 4, 24940, 0, '', '192.0.2.0/24',
  6, 443, toUInt16(32768 + hsh % 28000), 24, b, intDiv(b, 1400), 1, 'in'
FROM numbers({N} * R) WHERE m_ago <= 31;
