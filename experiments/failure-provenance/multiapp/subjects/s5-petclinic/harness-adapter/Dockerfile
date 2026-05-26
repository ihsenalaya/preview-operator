FROM springcommunity/spring-petclinic-rest:3.4.0

USER root
RUN apt-get update && apt-get install -y --no-install-recommends python3 python3-pip \
    && pip3 install requests --break-system-packages \
    && ln -sf /usr/bin/python3 /usr/local/bin/python \
    && apt-get clean && rm -rf /var/lib/apt/lists/*

COPY tests/ /app/tests/
COPY wrapper.py /wrapper.py

EXPOSE 9966
# Override Jib base ENTRYPOINT (which runs `java -cp ... PetClinicApplication`)
# so our wrapper.py runs and spawns Spring Boot itself.
ENTRYPOINT ["python3"]
CMD ["/wrapper.py"]
