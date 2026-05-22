FROM healthchecks/healthchecks:v3.6

USER root
RUN pip install requests --no-cache-dir --quiet

# Test scripts (contract: /app/tests/)
COPY tests/ /app/tests/
COPY wrapper.py /wrapper.py

EXPOSE 8000
CMD ["python3", "/wrapper.py"]
