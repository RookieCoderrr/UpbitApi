#!/bin/bash

docker stop upbit_server

docker container rm upbit_server

docker rmi upbit_server

docker-compose up -d
