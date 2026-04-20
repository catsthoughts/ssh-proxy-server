.PHONY: e2e-infra e2e-down e2e-logs e2e-test e2e-test-host e2e-rebuild e2e-clean e2e-full e2e-regen-keys

# Create e2e-keys directory if it doesn't exist
e2e-keys-init:
	@mkdir -p e2e/e2e-keys

# Start infrastructure only (ca + target + proxy) - no test-runner
e2e-infra: e2e-keys-init
	cd e2e && docker-compose up -d ca target proxy

# Rebuild proxy
e2e-rebuild:
	cd e2e && docker-compose build --no-cache proxy && docker-compose up -d --no-deps proxy

# Run tests inside container (old way)
e2e-test:
	cd e2e && docker-compose run --rm test-runner go test -v -count=1 -timeout=120s -run "^(TestClient|TestHost)" .

# Run tests on host machine (new way - for cross-platform testing)
e2e-test-host:
	cd e2e && go test -v -count=1 -timeout=120s -run "Host$$" .

# Full cycle: clean + build everything + run tests
e2e-full:
	cd e2e && docker-compose down -v && docker-compose build --no-cache && docker-compose up --abort-on-container-exit

# Regenerate keys and restart infrastructure
e2e-regen-keys: e2e-keys-init
	cd e2e && REGENERATE_KEYS=true docker-compose up -d ca target proxy && docker-compose restart proxy

# Show logs
e2e-logs:
	cd e2e && docker-compose logs -f

# Clean up everything
e2e-clean:
	cd e2e && docker-compose down -v && rm -rf e2e-keys

# Stop infrastructure but keep keys
e2e-stop:
	cd e2e && docker-compose stop

# Stop and remove infrastructure
e2e-down:
	cd e2e && docker-compose down