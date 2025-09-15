package shannon.task

# Vector-enhanced policy decisions. These rules are additive and only apply
# when vector context is present in input.

decision := {
    "allow": true,
    "reason": sprintf("approved based on %d similar successful queries", [count(input.similar_queries)]),
    "confidence": input.context_score
} {
    input.context_score > 0.8
    count(input.similar_queries) > 2
    some i
    input.similar_queries[i].outcome == "success"
}

decision := {
    "allow": false,
    "reason": "query pattern similar to previously denied requests",
    "require_approval": true
} {
    input.context_score > 0.9
    count(input.similar_queries) > 0
    some i
    input.similar_queries[i].outcome == "denied"
}

