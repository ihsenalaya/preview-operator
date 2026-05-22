FROM listmonk/listmonk:v2.5.1 AS listmonk-bin

FROM python:3.12-slim
WORKDIR /app

# Copy listmonk binary from the Go image
COPY --from=listmonk-bin /listmonk /listmonk

# Python test + proxy dependencies
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Test scripts (contract: must live at /app/tests/)
COPY tests/ ./tests/

# Proxy wrapper that starts listmonk and serves /healthz
COPY wrapper.py /wrapper.py

EXPOSE 9000
CMD ["python3", "/wrapper.py"]
