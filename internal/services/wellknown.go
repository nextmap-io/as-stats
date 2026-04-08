// Package services provides protocol and port name resolution.
package services

import "fmt"

// Protocols maps IANA protocol numbers to short names.
// https://www.iana.org/assignments/protocol-numbers/
var Protocols = map[uint8]string{
	0:   "HOPOPT",
	1:   "ICMP",
	2:   "IGMP",
	4:   "IPv4",
	6:   "TCP",
	17:  "UDP",
	27:  "RDP",
	33:  "DCCP",
	41:  "IPv6",
	46:  "RSVP",
	47:  "GRE",
	50:  "ESP",
	51:  "AH",
	58:  "ICMPv6",
	88:  "EIGRP",
	89:  "OSPF",
	103: "PIM",
	112: "VRRP",
	132: "SCTP",
	136: "UDP-Lite",
}

// ProtocolName returns the short name for a protocol number, or "proto:N" for unknown.
func ProtocolName(p uint8) string {
	if name, ok := Protocols[p]; ok {
		return name
	}
	return fmt.Sprintf("proto:%d", p)
}

// Services maps (protocol, port) to service name. Subset of IANA + common.
// The key encodes protocol in high byte for constant-time lookup.
type svcKey struct {
	proto uint8
	port  uint16
}

var Services = map[svcKey]string{
	// TCP
	{6, 20}:    "FTP-DATA",
	{6, 21}:    "FTP",
	{6, 22}:    "SSH",
	{6, 23}:    "Telnet",
	{6, 25}:    "SMTP",
	{6, 43}:    "WHOIS",
	{6, 53}:    "DNS",
	{6, 80}:    "HTTP",
	{6, 110}:   "POP3",
	{6, 111}:   "RPC",
	{6, 113}:   "IDENT",
	{6, 119}:   "NNTP",
	{6, 135}:   "MSRPC",
	{6, 139}:   "NetBIOS",
	{6, 143}:   "IMAP",
	{6, 179}:   "BGP",
	{6, 389}:   "LDAP",
	{6, 443}:   "HTTPS",
	{6, 445}:   "SMB",
	{6, 465}:   "SMTPS",
	{6, 514}:   "Syslog",
	{6, 587}:   "SMTP-Submit",
	{6, 636}:   "LDAPS",
	{6, 853}:   "DNS-over-TLS",
	{6, 873}:   "rsync",
	{6, 989}:   "FTPS-Data",
	{6, 990}:   "FTPS",
	{6, 993}:   "IMAPS",
	{6, 995}:   "POP3S",
	{6, 1080}:  "SOCKS",
	{6, 1194}:  "OpenVPN",
	{6, 1433}:  "MSSQL",
	{6, 1521}:  "Oracle",
	{6, 1723}:  "PPTP",
	{6, 2049}:  "NFS",
	{6, 2082}:  "cPanel",
	{6, 2083}:  "cPanel-SSL",
	{6, 2181}:  "ZooKeeper",
	{6, 2375}:  "Docker",
	{6, 2376}:  "Docker-TLS",
	{6, 3128}:  "Squid-Proxy",
	{6, 3306}:  "MySQL",
	{6, 3389}:  "RDP",
	{6, 4444}:  "Metasploit",
	{6, 5000}:  "UPnP",
	{6, 5060}:  "SIP",
	{6, 5222}:  "XMPP",
	{6, 5269}:  "XMPP-Server",
	{6, 5432}:  "PostgreSQL",
	{6, 5601}:  "Kibana",
	{6, 5672}:  "AMQP",
	{6, 5900}:  "VNC",
	{6, 5984}:  "CouchDB",
	{6, 6379}:  "Redis",
	{6, 6443}:  "Kubernetes-API",
	{6, 6660}:  "IRC",
	{6, 6661}:  "IRC",
	{6, 6662}:  "IRC",
	{6, 6663}:  "IRC",
	{6, 6664}:  "IRC",
	{6, 6665}:  "IRC",
	{6, 6666}:  "IRC",
	{6, 6667}:  "IRC",
	{6, 6668}:  "IRC",
	{6, 6669}:  "IRC",
	{6, 7001}:  "Cassandra",
	{6, 7077}:  "Spark",
	{6, 8000}:  "HTTP-Alt",
	{6, 8008}:  "HTTP-Alt",
	{6, 8080}:  "HTTP-Proxy",
	{6, 8081}:  "HTTP-Alt",
	{6, 8088}:  "HTTP-Alt",
	{6, 8443}:  "HTTPS-Alt",
	{6, 8883}:  "MQTT-TLS",
	{6, 9000}:  "HTTP-Alt",
	{6, 9090}:  "Prometheus",
	{6, 9092}:  "Kafka",
	{6, 9200}:  "Elasticsearch",
	{6, 9300}:  "Elasticsearch",
	{6, 9418}:  "Git",
	{6, 10050}: "Zabbix",
	{6, 10051}: "Zabbix-Trap",
	{6, 11211}: "Memcached",
	{6, 15672}: "RabbitMQ",
	{6, 25565}: "Minecraft",
	{6, 27017}: "MongoDB",
	{6, 27018}: "MongoDB",
	{6, 50070}: "Hadoop",

	// UDP
	{17, 53}:    "DNS",
	{17, 67}:    "DHCP-Server",
	{17, 68}:    "DHCP-Client",
	{17, 69}:    "TFTP",
	{17, 123}:   "NTP",
	{17, 137}:   "NetBIOS-NS",
	{17, 138}:   "NetBIOS-DGM",
	{17, 161}:   "SNMP",
	{17, 162}:   "SNMP-Trap",
	{17, 389}:   "LDAP",
	{17, 443}:   "HTTP/3",
	{17, 500}:   "IKE",
	{17, 514}:   "Syslog",
	{17, 520}:   "RIP",
	{17, 1194}:  "OpenVPN",
	{17, 1812}:  "RADIUS-Auth",
	{17, 1813}:  "RADIUS-Acct",
	{17, 1900}:  "SSDP",
	{17, 2055}:  "NetFlow",
	{17, 4500}:  "IPsec-NAT-T",
	{17, 5060}:  "SIP",
	{17, 5353}:  "mDNS",
	{17, 6343}:  "sFlow",
	{17, 11211}: "Memcached",
	{17, 27015}: "Source-Game",
	{17, 51820}: "WireGuard",
}

// ServiceName returns the service name for (protocol, port), or empty string.
func ServiceName(proto uint8, port uint16) string {
	if name, ok := Services[svcKey{proto, port}]; ok {
		return name
	}
	return ""
}
