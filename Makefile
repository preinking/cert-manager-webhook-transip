include Makefile.mk

.PHONY: stable clean help

.DEFAULT_GOAL := help

help:
	@echo "Available targets:"
	@echo "  build           - Build Docker image"
	@echo "  patch-release   - Create patch release (X.Y.Z → X.Y.Z+1)"
	@echo "  minor-release   - Create minor release (X.Y.Z → X.Y+1.0)"
	@echo "  major-release   - Create major release (X.Y.Z → X+1.0.0)"
	@echo "  stable          - Tag and push current version as stable"
	@echo "  showver         - Show current version"
	@echo "  push            - Push images to registry"
	@echo "  snapshot        - Quick build and push without version bump"
	@echo "  clean           - Remove built binaries and Docker images"
	@echo "  help            - Show this help message"

clean:
	@echo "Cleaning up built artifacts..."
	@rm -f webhook cert-manager-webhook-transip
	@docker rmi $(IMAGE):$(VERSION) 2>/dev/null || true
	@docker rmi $(IMAGE):latest 2>/dev/null || true
	@echo "Clean complete"

stable: check-status patch-release
	docker tag $(IMAGE):$(VERSION) $(IMAGE):stable
	docker push $(IMAGE):stable
