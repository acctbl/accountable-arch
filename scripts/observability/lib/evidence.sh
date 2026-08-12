#!/usr/bin/env bash

accountable_format_epoch() {
	local epoch="$1"
	date -u -d "@$epoch" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null ||
		date -u -r "$epoch" +%Y-%m-%dT%H:%M:%SZ
}

accountable_parse_instant() {
	local instant="$1"
	date -u -d "$instant" +%s 2>/dev/null ||
		date -u -j -f %Y-%m-%dT%H:%M:%SZ "$instant" +%s
}

accountable_evidence_age_seconds() {
	local instant="$1"
	local observed_epoch now_epoch
	observed_epoch="$(accountable_parse_instant "$instant")"
	now_epoch="$(date -u +%s)"
	echo "$((now_epoch - observed_epoch))"
}

accountable_require_fresh_capacity() {
	local evidence="$1"
	local maximum_age_seconds="${2:-1800}"
	if ! jq -e '
    .state == "pass" and
    (.observed_at | type) == "string" and
    (.observed_at | fromdateiso8601?) != null and
    (.irm_included_at_no_cost | type) == "boolean"
  ' "$evidence" >/dev/null; then
		echo "the reviewed capacity evidence is incomplete" >&2
		return 2
	fi
	local age
	age="$(accountable_evidence_age_seconds "$(jq -r .observed_at "$evidence")")"
	if ((age < 0 || age > maximum_age_seconds)); then
		echo "the reviewed capacity evidence is stale" >&2
		return 2
	fi
}
