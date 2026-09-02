PYTHON := .venv/bin/python

.PHONY: setup run plots plots-headless run-all run-all-headless test

setup:
	python3 -m venv .venv
	$(PYTHON) -m pip install --upgrade pip
	$(PYTHON) -m pip install -r requirements.txt

run:
	go run .

plots:
	@$(PYTHON) plot_3d.py & surface_pid=$$!; \
	$(PYTHON) plot_path.py & paths_pid=$$!; \
	wait $$surface_pid; \
	wait $$paths_pid

plots-headless:
	@$(PYTHON) plot_3d.py --no-show & surface_pid=$$!; \
	$(PYTHON) plot_path.py --no-show & paths_pid=$$!; \
	wait $$surface_pid; \
	wait $$paths_pid

run-all: run plots

run-all-headless: run plots-headless

test:
	go test -count=1 ./...
	go vet ./...
	go test -race -count=1 ./...
	$(PYTHON) -m unittest -v test_plots.py
	$(PYTHON) -m pip check
