#!/bin/bash

docker stop upbit_server

docker container rm upbit_server

docker rmi upbitapi_upbit

docker-compose up -d
