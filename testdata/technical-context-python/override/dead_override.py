from override.store import update_decision


def dead_override_decision():
    # This resembles the live control but has no production path and no
    # authorization or audit relationship.
    update_decision()
