# Makefile for Splunk Operator

IMAGE_TAG := "repo.splunk.com/releng/splunk-operator"

all: deps image

deps:
	@echo Checking vendor dependencies
	@dep ensure

image:
	@echo Building ${IMAGE_TAG}
	@operator-sdk build ${IMAGE_TAG}

push:
	@docker push ${IMAGE_TAG}
