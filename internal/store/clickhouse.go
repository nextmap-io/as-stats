package store

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/nextmap-io/as-stats/internal/config"
	"github.com/nextmap-io/as-stats/internal/model"
)

// ClickHouseStore implements FlowWriter, FlowReader, LinkStore, and ASNameStore.
type ClickHouseStore struct {
	conn driver.Conn
}

// NewClickHouseStore creates a new ClickHouse connection.
func NewClickHouseStore(cfg config.ClickHouseConfig) (*ClickHouseStore, error) {
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{cfg.Addr},
		Auth: clickhouse.Auth{
			Database: cfg.Database,
			Username: cfg.User,
			Password: cfg.Password,
		},
		Settings: clickhouse.Settings{
			"max_execution_time": 120,
		},
		MaxOpenConns:    30,
		MaxIdleConns:    15,
		ConnMaxLifetime: 10 * time.Minute,
		DialTimeout:     10 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("clickhouse open: %w", err)
	}

	if err := conn.Ping(context.Background()); err != nil {
		return nil, fmt.Errorf("clickhouse ping: %w", err)
	}

	return &ClickHouseStore{conn: conn}, nil
}

// WriteBatch inserts a batch of flow records into flows_raw.
func (s *ClickHouseStore) WriteBatch(ctx context.Context, flows []*model.FlowRecord) error {
	batch, err := s.conn.PrepareBatch(ctx, `INSERT INTO flows_raw (
		timestamp, router_ip, link_tag,
		src_ip, dst_ip, ip_version,
		src_as, dst_as,
		src_prefix, dst_prefix,
		protocol, src_port, dst_port, tcp_flags,
		bytes, packets,
		sampling_rate, direction, flow_type
	)`)
	if err != nil {
		return fmt.Errorf("prepare batch: %w", err)
	}

	for _, f := range flows {
		routerIP := ipToIPv6(f.RouterIP)
		srcIP := ipToIPv6(f.SrcIP)
		dstIP := ipToIPv6(f.DstIP)

		dir := ""
		switch f.Direction {
		case model.DirectionInbound:
			dir = "in"
		case model.DirectionOutbound:
			dir = "out"
		}

		err := batch.Append(
			f.Timestamp,
			routerIP,
			f.LinkTag,
			srcIP,
			dstIP,
			f.IPVersion,
			f.SrcAS,
			f.DstAS,
			f.SrcPrefix,
			f.DstPrefix,
			f.Protocol,
			f.SrcPort,
			f.DstPort,
			f.TCPFlags,
			f.Bytes,
			f.Packets,
			f.SamplingRate,
			dir,
			f.FlowType,
		)
		if err != nil {
			return fmt.Errorf("batch append: %w", err)
		}
	}

	return batch.Send()
}

// Close closes the ClickHouse connection.
func (s *ClickHouseStore) Close() error {
	return s.conn.Close()
}

// ipToIPv6 converts a net.IP to a 16-byte IPv6 representation.
func ipToIPv6(ip net.IP) net.IP {
	if ip == nil {
		return net.IPv6zero
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.To16()
	}
	if len(ip) == net.IPv6len {
		return ip
	}
	return net.IPv6zero
}
