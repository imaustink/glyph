# shellcheck shell=bash
# The harness's own assertions.

# A match near the top of a large rendered document (e.g. the migrations
# ConfigMap) must be found. Piping the text into `grep -q` let grep exit at
# the match while printf was still writing; printf died of SIGPIPE and, under
# pipefail, the match was reported as not found.
test_assert_contains_finds_an_early_match_in_large_text() {
  local big
  big="needle"$'\n'"$(head -c 400000 /dev/zero | tr '\0' 'x')"
  assert_contains "$big" '^needle' "an early match in 400 KB of text is found"
  assert_not_contains "$big" '^haystack' "an absent pattern is not found"
}
