export interface ASTraffic {
  as_number: number
  as_name: string
  bytes: number
  packets: number
  flows: number
  pct: number
}

export interface IPTraffic {
  ip: string
  as_number: number
  as_name: string
  bytes: number
  packets: number
  flows: number
}

export interface PrefixTraffic {
  prefix: string
  as_number: number
  as_name: string
  bytes: number
  packets: number
  flows: number
}

export interface TrafficPoint {
  t: string
  bytes_in: number
  bytes_out: number
  packets_in?: number
  packets_out?: number
}

export interface LinkTraffic {
  tag: string
  description: string
  capacity_mbps?: number
  bytes_in: number
  bytes_out: number
}

export interface ASInfo {
  number: number
  name: string
  country: string
}

export interface Overview {
  total_bytes_in: number
  total_bytes_out: number
  total_flows: number
  active_as_count: number
  top_as: ASTraffic[]
  top_ip: IPTraffic[]
  links: LinkTraffic[]
}

export interface ApiResponse<T> {
  data: T
  meta?: {
    from: string
    to: string
    total_bytes?: number
    limit?: number
    offset?: number
  }
  error?: string
}

export interface QueryFilters {
  from?: string
  to?: string
  period?: string
  link?: string
  direction?: string
  ip_version?: number
  limit?: number
  offset?: number
  q?: string
}

export interface ASDetailData {
  as_number: number
  as_name: string
  time_series: TrafficPoint[]
}

export interface IPDetailData {
  ip: string
  time_series: TrafficPoint[]
  top_as: ASTraffic[]
}

export interface LinkDetailData {
  tag: string
  time_series: TrafficPoint[]
  top_as: ASTraffic[]
}

export interface LinkTimeSeries {
  link_tag: string
  description: string
  points: TrafficPoint[]
}

export interface ASTrafficDetail {
  as_number: number
  as_name: string
  bytes: number
  series: LinkTimeSeries[]
}

export interface LinkConfig {
  tag: string
  router_ip: string
  snmp_index: number
  description: string
  capacity_mbps: number
}

export interface UserInfo {
  sub: string
  name: string
  email: string
  role: string
}
