FROM python:3.12-slim
WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY probe.py .
EXPOSE 9090
CMD ["python", "probe.py"]
