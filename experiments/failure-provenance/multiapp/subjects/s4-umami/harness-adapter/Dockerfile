FROM ghcr.io/umami-software/umami:postgresql-v2.15.1

USER root
RUN apk add --no-cache python3 py3-pip && pip3 install requests --break-system-packages 2>/dev/null || pip3 install requests

COPY tests/ /app/tests/
COPY wrapper.py /wrapper.py

EXPOSE 3000
CMD ["python3", "/wrapper.py"]
