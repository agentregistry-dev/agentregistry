# Example Guide - Hello World Template Reference

This reference document demonstrates how to structure comprehensive documentation that Claude loads when needed.

## Purpose

Reference files provide detailed information that would be too lengthy for SKILL.md. They are loaded into context only when Claude determines they're relevant to the task.

## When to Use This Guide

Load this guide when:
- Learning about reference documentation patterns
- Understanding detailed workflow steps
- Looking for comprehensive examples
- Needing troubleshooting information

## Detailed Workflow

### Phase 1: Preparation

**Step 1.1: Gather Requirements**
- Identify the task objective
- List required inputs
- Determine desired output format
- Note any constraints or requirements

**Step 1.2: Check Prerequisites**
- Verify required tools are available
- Ensure file paths are accessible
- Validate input data formats
- Check permission levels

### Phase 2: Execution

**Step 2.1: Run Initial Script**
```bash
python3 scripts/hello_world.py --name "Target" --format text
```

Expected output:
```
Hello, Target!
```

**Step 2.2: Process with Parameters**
```bash
python3 scripts/hello_world.py --name "World" --message "Greetings" --format json --verbose
```

Expected output:
```json
{
  "greeting": "Greetings, World!",
  "target": "World",
  "format": "json",
  "timestamp": "2025-11-10T12:00:00.000000"
}
```

**Step 2.3: Validate Results**
- Check output format matches expectations
- Verify all required fields are present
- Confirm data accuracy
- Test edge cases

### Phase 3: Finalization

**Step 3.1: Format Output**
- Apply templates if needed
- Standardize formatting
- Add metadata
- Prepare for distribution

**Step 3.2: Quality Check**
- Review completeness
- Verify correctness
- Check formatting consistency
- Test final output

## Code Examples

### Example 1: Basic Usage
```python
from scripts.hello_world import create_greeting

# Simple greeting
greeting = create_greeting("World")
print(greeting)  # Output: Hello, World!
```

### Example 2: Custom Message
```python
from scripts.hello_world import create_greeting

# Custom greeting
greeting = create_greeting("User", message="Welcome")
print(greeting)  # Output: Welcome, User!
```

### Example 3: Error Handling
```python
from scripts.hello_world import create_greeting

try:
    greeting = create_greeting("")  # Empty name
except ValueError as e:
    print(f"Error: {e}")
```

## API Reference

### Functions

#### create_greeting(name, message="Hello")

Create a formatted greeting string.

**Parameters:**
- `name` (str): Name to greet (required)
- `message` (str): Greeting word (default: "Hello")

**Returns:**
- str: Formatted greeting message

**Raises:**
- ValueError: If name is empty

**Example:**
```python
>>> create_greeting("World")
'Hello, World!'
>>> create_greeting("User", message="Hi")
'Hi, User!'
```

#### output_text(greeting, name, verbose=False)

Output greeting in plain text format.

**Parameters:**
- `greeting` (str): The greeting message
- `name` (str): Target name
- `verbose` (bool): Include metadata (default: False)

**Returns:**
- None (prints to stdout)

#### output_json(greeting, name, verbose=False)

Output greeting in JSON format.

**Parameters:**
- `greeting` (str): The greeting message
- `name` (str): Target name
- `verbose` (bool): Include timestamp (default: False)

**Returns:**
- None (prints JSON to stdout)

**JSON Schema:**
```json
{
  "greeting": "string",
  "target": "string",
  "format": "string",
  "timestamp": "string (optional, ISO 8601 format)"
}
```

## Configuration Options

### Output Formats

**Text Format**
- Simple plain text output
- Minimal overhead
- Human-readable
- Use for: Terminal display, log files

**JSON Format**
- Structured data output
- Machine-readable
- Easy to parse
- Use for: APIs, data processing, integration

**XML Format**
- Hierarchical structure
- Standards-compliant
- Widely supported
- Use for: Enterprise systems, SOAP APIs

### Verbosity Levels

**Normal Mode** (default)
- Essential output only
- Minimal information
- Fast processing

**Verbose Mode** (`--verbose`)
- Includes timestamps
- Shows metadata
- Useful for debugging
- Provides audit trail

## Troubleshooting

### Common Issues

#### Issue: "Error: Name cannot be empty"

**Cause:** Empty or whitespace-only name provided

**Solution:**
```bash
# Wrong
python3 scripts/hello_world.py --name ""

# Correct
python3 scripts/hello_world.py --name "World"
```

#### Issue: Script not found

**Cause:** Wrong working directory or incorrect path

**Solution:**
```bash
# Check current directory
pwd

# Navigate to skill directory
cd /path/to/hello-world-template

# Run with full path
python3 scripts/hello_world.py --name "World"
```

#### Issue: Permission denied

**Cause:** Script not executable

**Solution:**
```bash
# Make script executable
chmod +x scripts/hello_world.py

# Verify permissions
ls -l scripts/hello_world.py
```

### Debug Mode

For troubleshooting, run with Python's verbose flag:
```bash
python3 -v scripts/hello_world.py --name "World" --verbose
```

## Best Practices

### Script Usage

1. **Always specify required parameters**
```bash
   # Good
   python3 scripts/hello_world.py --name "World"
   
   # Bad - missing required parameter
   python3 scripts/hello_world.py
```

2. **Use appropriate format for context**
```bash
   # For human reading
   python3 scripts/hello_world.py --name "World" --format text
   
   # For programmatic processing
   python3 scripts/hello_world.py --name "World" --format json
```

3. **Enable verbose mode for debugging**
```bash
   python3 scripts/hello_world.py --name "World" --verbose
```

### Integration Patterns

**Pattern 1: Pipeline Processing**
```bash
# Generate greeting and process
python3 scripts/hello_world.py --name "World" --format json | jq '.greeting'
```

**Pattern 2: Batch Processing**
```bash
# Process multiple names
for name in Alice Bob Charlie; do
    python3 scripts/hello_world.py --name "$name"
done
```

**Pattern 3: Error Handling**
```bash
# Check exit code
if python3 scripts/hello_world.py --name "World"; then
    echo "Success"
else
    echo "Failed with code $?"
fi
```

## Advanced Topics

### Custom Extensions

The hello_world.py script can be extended for specific needs:

**Adding New Format:**
```python
def output_yaml(greeting, name, verbose=False):
    """Output greeting in YAML format."""
    print(f"greeting: {greeting}")
    print(f"target: {name}")
    print(f"format: yaml")
    if verbose:
        print(f"timestamp: {datetime.now().isoformat()}")
```

**Adding Validation:**
```python
def validate_name(name):
    """Validate name format."""
    if not name.isalpha():
        raise ValueError("Name must contain only letters")
    if len(name) > 50:
        raise ValueError("Name too long (max 50 characters)")
    return True
```

### Performance Considerations

For high-volume processing:

1. Use JSON format (faster parsing)
2. Disable verbose mode (less output)
3. Consider batch processing
4. Cache repeated operations

### Security Notes

- Sanitize input when accepting user data
- Validate all parameters
- Use appropriate file permissions
- Never execute untrusted code

## Additional Resources

- Python argparse documentation: https://docs.python.org/3/library/argparse.html
- JSON format specification: https://www.json.org/
- XML standards: https://www.w3.org/XML/

## Glossary

**Greeting**: A formatted message welcoming or addressing someone  
**Format**: The structure and encoding of output data  
**Verbose**: Detailed output mode including additional metadata  
**Exit Code**: Numeric value indicating script success (0) or failure (non-zero)  
**Parameter**: A value passed to a script to control its behavior
```
