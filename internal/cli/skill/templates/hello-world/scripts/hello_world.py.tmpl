#!/usr/bin/env python3
"""
Hello World Script - Template demonstrating script best practices

This script shows examples of:
- Command-line argument parsing
- Multiple output formats
- Error handling
- Documentation
- Exit codes

Usage:
    python3 hello_world.py --name "World"
    python3 hello_world.py --name "World" --format json
    python3 hello_world.py --name "World" --message "Welcome" --verbose
"""

import sys
import json
import argparse
from datetime import datetime


def create_greeting(name, message="Hello"):
    """
    Create a greeting message.

    Args:
        name: Name to greet
        message: Greeting word (default: "Hello")

    Returns:
        Formatted greeting string
    """
    return f"{message}, {name}!"


def output_text(greeting, name, verbose=False):
    """Output greeting in plain text format."""
    print(greeting)
    if verbose:
        print(f"Generated at: {datetime.now().isoformat()}")
        print(f"Target: {name}")


def output_json(greeting, name, verbose=False):
    """Output greeting in JSON format."""
    result = {
        "greeting": greeting,
        "target": name,
        "format": "json"
    }
    if verbose:
        result["timestamp"] = datetime.now().isoformat()

    print(json.dumps(result, indent=2))


def output_xml(greeting, name, verbose=False):
    """Output greeting in XML format."""
    xml = f"""<?xml version="1.0" encoding="UTF-8"?>
<greeting>
    <message>{greeting}</message>
    <target>{name}</target>
    <format>xml</format>"""

    if verbose:
        xml += f"\n    <timestamp>{datetime.now().isoformat()}</timestamp>"

    xml += "\n</greeting>"
    print(xml)


def main():
    """Main entry point for the script."""
    # Set up argument parser
    parser = argparse.ArgumentParser(
        description="Generate a hello world greeting in various formats",
        epilog="Example: python3 hello_world.py --name World --format json"
    )

    parser.add_argument(
        "--name",
        required=True,
        help="Name to greet"
    )

    parser.add_argument(
        "--message",
        default="Hello",
        help="Greeting word (default: Hello)"
    )

    parser.add_argument(
        "--format",
        choices=["text", "json", "xml"],
        default="text",
        help="Output format (default: text)"
    )

    parser.add_argument(
        "--verbose",
        action="store_true",
        help="Include additional metadata in output"
    )

    # Parse arguments
    try:
        args = parser.parse_args()
    except SystemExit as e:
        # argparse calls sys.exit on error, catch it to return proper exit code
        return e.code if e.code else 1

    # Validate name
    if not args.name.strip():
        print("Error: Name cannot be empty", file=sys.stderr)
        return 1

    # Create greeting
    try:
        greeting = create_greeting(args.name, args.message)
    except Exception as e:
        print(f"Error creating greeting: {e}", file=sys.stderr)
        return 1

    # Output in requested format
    try:
        if args.format == "json":
            output_json(greeting, args.name, args.verbose)
        elif args.format == "xml":
            output_xml(greeting, args.name, args.verbose)
        else:
            output_text(greeting, args.name, args.verbose)
    except Exception as e:
        print(f"Error generating output: {e}", file=sys.stderr)
        return 1

    return 0


if __name__ == "__main__":
    exit_code = main()
    sys.exit(exit_code)