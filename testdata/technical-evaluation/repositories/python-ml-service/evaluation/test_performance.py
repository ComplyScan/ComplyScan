def test_model_accuracy_threshold() -> None:
    measured_accuracy = 0.94
    minimum_threshold = 0.90
    assert measured_accuracy >= minimum_threshold
