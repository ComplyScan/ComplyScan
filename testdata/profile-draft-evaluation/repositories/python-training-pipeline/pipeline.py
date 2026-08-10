from datasets import load_dataset
from transformers import Trainer


def load_customer_training_data(path: str):
    dataset = load_dataset("csv", data_files=path)
    required_columns = {"customer_email", "request_text", "label"}
    if not required_columns.issubset(dataset["train"].column_names):
        raise ValueError("training data schema is incomplete")
    return dataset


def train_and_evaluate(model, dataset):
    trainer = Trainer(model=model, train_dataset=dataset["train"], eval_dataset=dataset["test"])
    trainer.train()
    return trainer.evaluate()
