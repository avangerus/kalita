# One image for all kalita Python workers (stdlib only, zero deps).
# The entrypoint script selects which worker to run.
FROM python:3.12-slim
WORKDIR /workers
COPY . /workers
# no pip install — workers are stdlib-only by design
ENTRYPOINT ["python", "-u"]
