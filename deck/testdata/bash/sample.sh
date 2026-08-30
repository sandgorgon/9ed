#!/bin/bash
# sample.sh — fixture for BashSegmenter's golden test.
set -euo pipefail

# greet prints a hello to the named person.
greet() {
	echo "hello, $1"
}

# cleanup removes the scratch directory, brace on its own line.
cleanup()
{
	rm -rf "$SCRATCH_DIR"
}

greet "$1"
cleanup
