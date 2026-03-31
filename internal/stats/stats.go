// Package stats provides aggregation functions for computing analytics
// from OpenCode's database.
//
// Stats domains:
// - Overview: Total sessions, messages, cost, tokens
// - Daily: Per-day breakdown over 7d/30d periods
// - Models: Usage by model (cost, tokens, sessions)
// - Tools: Tool invocation statistics
// - Projects: Per-project aggregation
// - Sessions: Paginated session list with summary info
// - SessionByID: Detailed view with message breakdown
// - Config: OpenCode configuration view
package stats
