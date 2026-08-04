from override.store import persist_result


def dead_override_decision():
    # This resembles the live control but has no production path and no
    # authorization or audit relationship.
    persist_result()
