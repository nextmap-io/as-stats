// Package bgp implements RFC 7999 blackholing for as-stats.
//
// The API process owns everything: a Service holds the active blocks
// (persisted in bgp_blocks), checks every request against the safety rails
// (Policy: allow-list, protected addresses, prefix length; Limits: max
// active, auto-blocks per hour, duration), drives the BGP daemon through a
// Daemon (CommandDaemon runs gobgp / birdc / vtysh / a script without a
// shell; DryRunDaemon only logs), withdraws expired blocks and reconciles
// the table with the daemon's RIB.
//
// The collector's alert engine never talks to the daemon: Client posts
// auto-blocks to the API's /internal/bgp/blocks route, authenticated with
// the shared BGP_API_TOKEN.
package bgp
