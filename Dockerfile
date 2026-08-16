FROM python:3.12-slim

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        iproute2 \
        network-manager \
        libsystemd0 \
        util-linux \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY ipflip/ ipflip/
COPY pyproject.toml .
RUN pip install --no-cache-dir -e .

ENV USE_SUDO=false
ENV DBUS_SYSTEM_BUS_ADDRESS=unix:path=/host/run/dbus/system_bus_socket

EXPOSE 5000

CMD ["ipflip", "serve"]
