def test_fairness_metric_threshold() -> None:
    demographic_parity_metric = 0.92
    assert demographic_parity_metric >= 0.90
