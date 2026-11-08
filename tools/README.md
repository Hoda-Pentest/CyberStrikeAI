# tool configuration file notes

## Overview

Each tool has its own configuration file, stored in the `tools/` directory. This approach makes tool configuration clearer and easier to maintain and manage. The system automatically loads all `.yaml` and `.yml` files in the `tools/` directory.

## Configuration file format

Each tool configuration file is a YAML file. The table below lists the currently supported top-level fields and whether they are required; it is recommended to check each item before submitting:

| Field | Required | Type | Description |
|------|------|------|------|
| `name` | ✅ | string | Unique tool identifier; a combination of lowercase letters, digits, and hyphens is recommended. |
| `command` | ✅ | string | The actual command or script name to execute; must be in the system PATH or written as an absolute path. |
| `enabled` | ✅ | bool | Whether to register with MCP; when set to `false`, the tool is ignored. |
| `description` | ✅ | string | Detailed description, supports multi-line Markdown, for the AI's deep understanding and `resources/read` queries. |
| `short_description` | Optional | string | A 20-50 character summary, used for the tool list and to reduce token consumption; when missing, the beginning of `description` is automatically taken. |
| `args` | Optional | string[] | Fixed arguments, prepended to the command line in order, commonly used to define the default scan mode. |
| `parameters` | Optional | array | List of runtime-configurable parameters; see the "Parameter definitions" section for details. |
| `arg_mapping` | Optional | string | Parameter mapping mode (`auto`/`manual`/`template`), default `auto`; no need to fill in unless there is a special need. |

> If a field is filled in incorrectly or a required item is missing, the system skips that tool at load time and outputs a warning in the log, but it does not affect other tools.

## Tool description

### Short description (`short_description`)

- **Purpose**: used for the tool list, reduces the token consumption sent to the large model
- **Requirement**: one sentence (20-50 characters) describing the tool's core purpose
- **Example**: `"Network scanning tool for discovering network hosts, open ports, and services"`

### Detailed description (`description`)

Supports multi-line text, and should include:

1. **Tool function description**: the tool's main functionality
2. **Use cases**: under what circumstances to use this tool
3. **Notes**: notes and warnings for use
4. **Examples**: usage examples (optional)

**Important note**:
- When the tool list is sent to the large model, `short_description` is used (if present)
- If there is no `short_description`, the system automatically extracts the first line or first 100 characters from `description`
- The detailed description can be obtained via MCP's `resources/read` interface (URI: `tool://tool_name`)

This can significantly reduce token consumption, especially when there are many tools (e.g. 100 tools).

## Parameter definitions

Each parameter can include the following fields:

- `name`: parameter name
- `type`: parameter type (string, int, bool, array)
- `description`: detailed parameter description (supports multiple lines)
- `required`: whether it is required (true/false)
- `default`: default value
- `flag`: command-line flag (e.g. "-u", "--url", "-p")
- `position`: position of the positional parameter (integer, starting from 0)
- `format`: parameter format ("flag", "positional", "combined", "template")
- `template`: template string (for format="template")
- `options`: list of possible values (for enum types)

### Parameter format notes

- **`flag`**: flag parameter, formatted as `--flag value` or `-f value`
  - Example:`flag: "-u"` → `-u http://example.com`
  
- **`positional`**: positional parameter, added to the command in order
  - Example: `position: 0` → used as the first positional parameter
  
- **`combined`**: combined format, formatted as `--flag=value`
  - Example:`flag: "--level"`, `format: "combined"` → `--level=3`
  
- **`template`**: template format, uses a custom template string
  - Example: `template: "{flag} {value}"` → custom format

### Special parameters

#### `additional_args` parameter

`additional_args` is a special parameter used to pass extra command-line options not defined in the parameter list. This parameter is parsed and split by spaces into multiple arguments.

**Use Cases:**
- Pass advanced options to the tool
- Pass parameters not defined in the configuration
- Pass complex parameter combinations

**Example:**
```yaml
- name: "additional_args"
  type: "string"
  description: "Additional tool parameters, multiple parameters separated by spaces"
  required: false
  format: "positional"
```

**Usage Examples:**
- `additional_args: "--script vuln -O"` → parsed as `["--script", "vuln", "-O"]`
- `additional_args: "-T4 --max-retries 3"` → parsed as `["-T4", "--max-retries", "3"]`

**Notes:**
- Parameters are split by spaces, but content within quotes is preserved
- Ensure the parameter format is correct to avoid command injection risk
- This parameter is appended to the end of the command

#### `scan_type` parameter (specific tools)

Some tools (such as `nmap`) support the `scan_type` parameter, used to override the default scan-type parameter.

**Example (nmap):**
```yaml
- name: "scan_type"
  type: "string"
  description: "scan type option, can override the default scan type"
  required: false
  format: "positional"
```

**Usage Examples:**
- `scan_type: "-sV -sC"` → version detection and script scanning
- `scan_type: "-A"` → comprehensive scan

**Notes:**
- If `scan_type` is specified, it replaces the default scan-type parameter in the tool configuration
- Separate multiple options with spaces

### Parameter description requirements

The parameter description should include:

1. **Parameter purpose**: what this parameter does
2. **Format requirements**: the format requirements for the parameter value (e.g. URL format, port range format, etc.)
3. **Example values**: specific example values (display multiple examples as a list)
4. **Notes**: things to note when using (permission requirements, performance impact, security warnings, etc.)

**Description format suggestions:**
- Use Markdown formatting to enhance readability
- Use `**bold**` to highlight important information
- Use lists to display multiple examples or options
- Use code blocks to display complex formats

**Example:**
```yaml
description: |
  Target IP address or domain name. Can be a single IP, an IP range, CIDR format, or a domain name.
  
  **Example Values:**
  - Single IP: "192.168.1.1"
  - IP range: "192.168.1.1-100"
  - CIDR: "192.168.1.0/24"
  - Domain: "example.com"
  
  **Notes:**
  - Ensure the target address format is correct
  - Required parameter, cannot be empty
```

## Parameter type notes

### Boolean type (bool)

Boolean-type parameters have special handling:
- `true`: add only the flag, not a value (e.g. `--flag`)
- `false`: do not add any parameter
- Supports multiple input formats: `true`/`false`, `1`/`0`, `"true"`/`"false"`

**Example:**
```yaml
- name: "verbose"
  type: "bool"
  description: "Verbose output mode"
  required: false
  default: false
  flag: "-v"
  format: "flag"
```

### String type (string)

The most commonly used parameter type, supports any string value.

### Integer type (int/integer)

Used for numeric parameters, such as port numbers, levels, etc.

**Example:**
```yaml
- name: "level"
  type: "int"
  description: "test level, range 1-5"
  required: false
  default: 3
  flag: "--level"
  format: "combined"  # --level=3
```

### Array type (array)

Arrays are automatically converted to a comma-separated string.

**Example:**
```yaml
- name: "ports"
  type: "array"
  item_type: "number"
  description: "port list"
  required: false
  # input: [80, 443, 8080]
  # output: "80,443,8080"
```

## Examples

Refer to the existing tool configuration files in the `tools/` directory:

- `nmap.yaml`: network scanning tool (includes `scan_type` and `additional_args` examples)
- `sqlmap.yaml`: SQL injection detection tool (includes an `additional_args` example)
- `nikto.yaml`: web server scanning tool
- `dirb.yaml`: web directory scanning tool
- `exec.yaml`: system command execution tool

### Full example: nmap tool configuration

```yaml
name: "nmap"
command: "nmap"
args: ["-sT", "-sV", "-sC"]  # default scan type
enabled: true

short_description: "network scanning tool, used to discover network hosts, open ports, and services"

description: |
  Network mapping and port scanning tool, used to discover hosts, services, and open ports on a network.
  
  **Key Features:**
  - Host discovery: detect active hosts in the network
  - Port scanning: identify open ports on the target host
  - Service identification: detect the type and version of services running on ports
  - Operating system detection: identify the operating system type of the target host
  - Vulnerability detection: use NSE scripts to detect common vulnerabilities

parameters:
  - name: "target"
    type: "string"
    description: "target IP address or domain name"
    required: true
    position: 0
    format: "positional"
  
  - name: "ports"
    type: "string"
    description: "port range, e.g.: 1-1000"
    required: false
    flag: "-p"
    format: "flag"
  
  - name: "scan_type"
    type: "string"
    description: "scan type option, e.g.: '-sV -sC'"
    required: false
    format: "positional"
  
  - name: "additional_args"
    type: "string"
    description: "extra Nmap arguments, e.g.: '--script vuln -O'"
    required: false
    format: "positional"
```

## Adding a new tool

To add a new tool, simply create a new YAML file in the `tools/` directory, for example `my_tool.yaml`:

```yaml
name: "my_tool"
command: "my-command"
args: ["--default-arg"]  # fixed arguments (optional)
enabled: true

# short description (recommended) - used for the tool list, reduces token consumption
short_description: "a one-sentence description of the tool's purpose"

# detailed description - used for tool documentation and AI understanding
description: |
  Detailed tool description, supports multi-line text and Markdown format.
  
  **Key Features:**
  - Feature 1
  - Feature 2
  
  **Use Cases:**
  - Scenario 1
  - Scenario 2
  
  **Notes:**
  - Notes for use
  - Permission requirements
  - Performance impact

parameters:
  - name: "target"
    type: "string"
    description: |
      Detailed description of the target parameter.
      
      **Example Values:**
      - "value1"
      - "value2"
      
      **Notes:**
      - Format requirements
      - Usage limitations
    required: true
    position: 0  # positional parameter
    format: "positional"
  
  - name: "option"
    type: "string"
    description: "option parameter description"
    required: false
    flag: "--option"
    format: "flag"
  
  - name: "verbose"
    type: "bool"
    description: "Verbose output mode"
    required: false
    default: false
    flag: "-v"
    format: "flag"
  
  - name: "additional_args"
    type: "string"
    description: "Additional tool parameters, multiple parameters separated by spaces"
    required: false
    format: "positional"
```

After saving the file, restart the service to automatically load the new tool.

### Tool configuration best practices

1. **Parameter design**
   - Define commonly used parameters separately to make them easier for the AI to understand and use
   - Use `additional_args` for flexibility, supporting advanced usage
   - Provide clear descriptions and examples for parameters

2. **Description optimization**
   - Use `short_description` to reduce token consumption
   - `description` should be detailed to help the AI understand the tool's purpose
   - Use Markdown formatting to enhance readability

3. **Default value setting**
   - Set reasonable default values for commonly used parameters
   - The default value of a boolean type is usually set to `false`
   - Set numeric types according to the tool's characteristics

4. **Parameter validation**
   - Clearly state parameter format requirements in the description
   - Provide multiple example values
   - Explain the parameter's limitations and notes

5. **Security**
   - For dangerous operations, add a warning to the description
   - Explain the permission requirements
   - Reminder: use only in authorized environments

6. **Single execution time and timeout (best practices)**
   - If a tool often runs for a long time (e.g. still showing "executing" after more than 10-30 minutes), it is an abnormally long hang; recommendations:
     - Set the maximum execution time for a single tool in `agent.tool_timeout_minutes` in **config.yaml** (default 10 minutes); after a timeout it is automatically terminated and resources are released;
     - Increase this value appropriately when a longer scan is needed (e.g. 20, 30); setting it to 0 (unlimited) is not recommended;
     - On the task monitoring page you can use "Stop task" to interrupt the current conversation and subsequent tool calls for the entire task;
     - Tool implementations should support being "interruptible" or have a built-in timeout (e.g. a timeout set within the script) as much as possible, to work together with the system timeout.

## Disabling a tool

To disable a tool, simply set the `enabled` field in its configuration file to `false`, or directly delete/rename the configuration file.

Once disabled, the tool does not appear in the tool list and the AI cannot invoke it.

## Tool configuration validation

The system performs basic validation when loading tool configurations:

- ✅ Check required fields (`name`, `command`, `enabled`)
- ✅ Validate the parameter definition format
- ✅ Check whether the parameter type is supported

If the configuration is incorrect, the system displays a warning message in the startup log but does not prevent the server from starting. Incorrect tool configurations are skipped, and other tools can still be used normally.

## FAQ

### Q: How do I pass multiple parameter values?

A: For array-type parameters, the system automatically converts them to a comma-separated string. For cases where multiple independent parameters need to be passed, you can use the `additional_args` parameter.

### Q: How do I override a tool's default parameters?

A: Some tools (such as `nmap`) support the `scan_type` parameter to override the default scan type. For other cases, you can use the `additional_args` parameter.

### Q: What if a tool runs for over 30 minutes and keeps showing "executing"?

A: This is an abnormally long hang; recommendations:
1. Configure `agent.tool_timeout_minutes` in **config.yaml** (default 10); a single tool that exceeds that number of minutes is automatically terminated;
2. On the monitoring page, use "Stop task" on this task to interrupt it immediately;
3. If the tool genuinely needs more time, you can increase `tool_timeout_minutes` appropriately, but setting it to 0 is not recommended.

### Q: What if tool execution fails?

A: Check the following points:
1. Whether the tool is installed and in the system PATH
2. Whether the tool configuration is correct
3. Whether the parameter format meets the requirements
4. Check the server logs for detailed error information

### Q: How do I test a tool configuration?

A: You can use the `cmd/test-config/main.go` tool to test configuration loading:
```bash
go run cmd/test-config/main.go
```

### Q: How is parameter order controlled?

A: Use the `position` field to control the order of positional parameters. **The parameter at position 0 (e.g. gobuster's `dir` subcommand) comes immediately after the command name and before all flag parameters**, to be compatible with CLIs that require a "subcommand + options" form. The remaining flag parameters are added in the order they appear in the `parameters` list, and then the remaining positional parameters are added by position 1, 2, etc. `additional_args` is appended to the end of the command.

## Tool configuration template

### Basic tool template

```yaml
name: "tool_name"
command: "command"
enabled: true

short_description: "short description (20-50 characters)"

description: |
  Detailed description, explaining the tool's functionality, use cases, and notes.

parameters:
  - name: "target"
    type: "string"
    description: "target parameter description"
    required: true
    position: 0
    format: "positional"
  
  - name: "additional_args"
    type: "string"
    description: "extra tool arguments"
    required: false
    format: "positional"
```

### Tool template with flag parameters

```yaml
name: "tool_name"
command: "command"
enabled: true

short_description: "short description"

description: |
  Detailed description.

parameters:
  - name: "target"
    type: "string"
    description: "target"
    required: true
    flag: "-t"
    format: "flag"
  
  - name: "option"
    type: "bool"
    description: "option"
    required: false
    default: false
    flag: "--option"
    format: "flag"
  
  - name: "level"
    type: "int"
    description: "level"
    required: false
    default: 3
    flag: "--level"
    format: "combined"
  
  - name: "additional_args"
    type: "string"
    description: "extra arguments"
    required: false
    format: "positional"
```

## Related documentation

- Main project README: see `README.md` for the complete project documentation
- Tool list: see all tool configuration files in the `tools/` directory
- API documentation: see the API interface notes in the main README

