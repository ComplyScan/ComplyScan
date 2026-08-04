from fastapi import FastAPI

from pipeline.dataset_validation import validate_training_dataset

app = FastAPI()


@app.post("/datasets/validate")
def validate_dataset_route(dataset: list[dict]) -> int:
    return validate_training_dataset(dataset)
