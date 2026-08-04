def validate_training_dataset(dataset: list[dict]) -> int:
    required_schema = {"text", "label"}
    missing = [row for row in dataset if not required_schema.issubset(row)]
    if missing:
        raise ValueError("training dataset has missing schema fields")
    return len(dataset)
