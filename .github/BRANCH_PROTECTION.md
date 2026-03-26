# Branch Protection Configuration
# ================================
# Apply these settings via GitHub UI or API after deployment:
#
# Branch: main
# ─────────────
# ✅ Require pull request before merging
#   - Required approving reviews: 1
#   - Dismiss stale reviews on new pushes
#   - Require review from Code Owners
# ✅ Require status checks to pass
#   - Required checks: Go Lint, Go Test, Frontend Lint, Frontend Typecheck, Frontend Build
#   - Require branches to be up to date
# ✅ Require signed commits (recommended)
# ✅ Do not allow bypassing the above settings
# ✅ Restrict deletions
# ❌ Allow force pushes: NO
#
# Tag protection: v*
# ─────────────────
# Only allow tags matching v* to be created by admins
#
# To apply via GitHub API:
#
#   # Branch protection on main
#   curl -X PUT \
#     -H "Authorization: token $GITHUB_TOKEN" \
#     -H "Accept: application/vnd.github+json" \
#     https://api.github.com/repos/nextmap-io/as-stats/branches/main/protection \
#     -d '{
#       "required_status_checks": {
#         "strict": true,
#         "contexts": ["Go Lint", "Go Test", "Frontend Lint", "Frontend Typecheck", "Frontend Build"]
#       },
#       "enforce_admins": true,
#       "required_pull_request_reviews": {
#         "required_approving_review_count": 1,
#         "dismiss_stale_reviews": true,
#         "require_code_owner_reviews": true
#       },
#       "restrictions": null,
#       "allow_force_pushes": false,
#       "allow_deletions": false
#     }'
#
#   # Tag protection
#   curl -X POST \
#     -H "Authorization: token $GITHUB_TOKEN" \
#     -H "Accept: application/vnd.github+json" \
#     https://api.github.com/repos/nextmap-io/as-stats/tags/protection \
#     -d '{"pattern": "v*"}'
