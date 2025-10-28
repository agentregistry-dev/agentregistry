#!/bin/bash

# Setup script for arctl
# This script helps you get started quickly

set -e

echo "🚀 Setting up arctl..."
echo ""

# Check prerequisites
echo "Checking prerequisites..."

if ! command -v go &> /dev/null; then
    echo "❌ Go is not installed. Please install Go 1.22 or later."
    exit 1
fi
echo "✓ Go found: $(go version)"

if ! command -v node &> /dev/null; then
    echo "❌ Node.js is not installed. Please install Node.js 18 or later."
    exit 1
fi
echo "✓ Node.js found: $(node --version)"

if ! command -v npm &> /dev/null; then
    echo "❌ npm is not installed. Please install npm."
    exit 1
fi
echo "✓ npm found: $(npm --version)"

echo ""
echo "All prerequisites satisfied!"
echo ""

# Download Go dependencies
echo "📦 Downloading Go dependencies..."
go mod download
echo "✓ Go dependencies downloaded"
echo ""

# Install UI dependencies
echo "📦 Installing UI dependencies..."
cd ui
npm install
cd ..
echo "✓ UI dependencies installed"
echo ""

# Build UI
echo "🏗️  Building Next.js UI..."
cd ui
npm run build
cd ..
echo "✓ UI built successfully"
echo ""

# Build Go CLI
echo "🏗️  Building Go CLI..."
go build -o bin/arctl main.go
echo "✓ CLI built successfully"
echo ""

# Test the binary
echo "Testing the binary..."
./bin/arctl version
echo ""

echo "✅ Setup complete!"
echo ""
echo "Quick start:"
echo "  ./bin/arctl --help         # Show all commands"
echo "  ./bin/arctl ui             # Launch web UI"
echo "  ./bin/arctl version        # Show version"