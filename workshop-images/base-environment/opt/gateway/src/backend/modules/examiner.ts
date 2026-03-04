import * as child_process from "child_process"
import * as express from "express"
import * as fs from "fs"
import * as glob from "glob"
import * as os from "os"
import * as path from "path"

import { config } from "./config"

const test_program_directory_patterns = [
    "/home/eduk8s/workshop/examiner/tests",
    "/opt/workshop/examiner/tests",
    "/opt/packages/*/examiner/tests",
    "/opt/eduk8s/workshop/examiner/tests",
    "/opt/renderer/workshop/examiner/tests"
];

/**
 * Expands glob patterns in the test program directory list to get actual directories.
 * This is called each time we need to search for a test program to handle directories
 * that may be added after the application has started.
 */
function get_test_program_directories(): string[] {
    const directories: string[] = []

    for (const pattern of test_program_directory_patterns) {
        // If pattern contains glob characters, expand it
        if (pattern.includes("*") || pattern.includes("?") || pattern.includes("[")) {
            directories.push(...glob.sync(pattern))
        } else {
            directories.push(pattern)
        }
    }

    return directories
}

/**
 * Validates that a test name contains only safe characters.
 * Allows alphanumeric characters, hyphens, underscores, dots, forward slashes
 * (for subdirectory grouping), colons, at signs, and plus signs.
 */
function validate_test_name(name: string): boolean {
    if (!name || typeof name !== "string") {
        return false
    }

    // Only allow alphanumeric, hyphens, underscores, dots, forward slashes, colons, at signs, and plus signs
    const safePattern = /^[a-zA-Z0-9._/:@+-]+$/
    return safePattern.test(name)
}

/**
 * Validates that a canonical pathname is within the allowed directory.
 * The pathname should already be resolved via fs.realpathSync() which follows
 * symlinks and resolves all path components to produce an absolute canonical path.
 */
function validate_pathname(canonicalPath: string, canonicalDir: string): boolean {
    if (!canonicalPath || typeof canonicalPath !== "string") {
        return false
    }

    // Check if canonicalPath starts with the directory followed by path separator
    return canonicalPath.startsWith(canonicalDir + path.sep)
}

/**
 * Validates command arguments.
 * Ensures args is an array of strings without null bytes.
 * Empty strings are allowed as they are valid arguments.
 */
function validate_args(args: any): string[] | null {
    if (!Array.isArray(args)) {
        return null
    }

    const validatedArgs: string[] = []

    for (const arg of args) {
        if (typeof arg !== "string") {
            return null
        }

        // Reject strings with null bytes (injection vector)
        if (arg.includes("\0")) {
            return null
        }

        validatedArgs.push(arg)
    }

    return validatedArgs
}

/**
 * Locate a test program inside allowed directories.
 * Performs name validation, canonical path resolution, and boundary validation.
 */
function find_test_program(name: string): string | null {
    if (!validate_test_name(name)) {
        console.error(`Invalid test name: ${name}`)
        return null
    }

    for (const dir of get_test_program_directories()) {
        const potentialPath = path.join(dir, name)

        try {
            // Check for file existence and executability
            fs.accessSync(potentialPath, fs.constants.R_OK | fs.constants.X_OK)

            // Resolve the file path to its canonical form (follows symlinks, resolves ..)
            const canonicalPath = fs.realpathSync(potentialPath)

            // Validate the canonical path to ensure it is within the allowed directory.
            // Use the original dir value (not canonicalised) to ensure the allowed
            // directory path itself does not contain symlinks.
            if (!validate_pathname(canonicalPath, dir)) {
                console.error(`Security alert: Path ${potentialPath} resolved to unauthorized location ${canonicalPath}`)
                continue
            }

            return canonicalPath
        } catch (err) {
            // File doesn't exist or isn't accessible, continue searching
        }
    }

    return null
}

export function setup_examiner(app: express.Application, token: string = null) {
    if (!config.enable_examiner)
        return

    app.use("/examiner/test/", express.json());

    async function examiner_test(req, res, next) {
        // Use params[0] for wildcard route that captures paths with slashes
        let test = req.params[0]

        let options = req.body || {}

        let timeout = options.timeout || 15
        let form = options.form || {}

        // Validate and sanitize args
        let args = validate_args(options.args || [])

        if (args === null) {
            console.error(`${test}: Invalid arguments provided`)
            return res.status(400).json({
                success: false,
                message: "Invalid arguments: must be an array of strings"
            })
        }

        if (!test)
            return next()

        let pathname = find_test_program(test)

        if (!pathname)
            return res.sendStatus(404)

        let process: any

        try {
            let timer: any

            process = child_process.spawn(pathname, args, { cwd: os.homedir() })

            process.on('error', (err) => {
                console.error(`${test}: Test failed to execute - ${err}`)

                let result = {
                    success: false,
                    message: "Test failed to execute"
                }

                return res.json(result)
            })

            process.on('exit', (code) => {
                console.log(`${test}: Exited with status ${code}`)

                if (timer)
                    clearTimeout(timer)

                let result = {
                    success: true,
                    message: "Test successfully completed"
                }

                if (code !== 0) {
                    result["success"] = false

                    if (code === null)
                        result["message"] = "Process killed or crashed"
                    else
                        result["message"] = "Test failed to complete"
                }

                return res.json(result)
            })

            process.on('spawn', () => {
                console.log(`${test}: Spawned successfully`)

                if (form) {
                    process.stdin.setEncoding('utf-8')
                    process.stdin.on('error', (error) => console.log(`${test}: Error writing to stdin - ${error}`));
                    process.stdin.write(JSON.stringify(form))
                }

                process.stdin.end()
            })

            // Capture examiner script output to a log file.

            const logFilePath = path.join(os.homedir(), ".local/share/workshop/examiner-scripts.log")
            const logStream = fs.createWriteStream(logFilePath, { flags: "a" });

            logStream.on('error', (err) => {
                // Ignore the error to prevent EPIPE error when writing data.
            });

            process.stdout.on('data', (data) => {
                const lines = data.toString().split('\n');
                lines.forEach((line) => {
                    const logData = `${test}: ${line}`;
                    console.log(logData);
                    logStream.write(logData + '\n'); // Append stdout to the log file.
                });
            });

            process.stderr.on('data', (data) => {
                const lines = data.toString().split('\n');
                lines.forEach((line) => {
                    const logData = `${test}: ${line}`;
                    console.log(logData);
                    logStream.write(logData + '\n'); // Append stderr to the log file.
                });
            });

            if (timeout) {
                console.log(`${test}: timeout=${options.timeout}`)
                timer = setTimeout(() => {
                    console.error(`${test}: Test timeout expired`)
                    process.kill()
                }, timeout * 1000)
            }
        } catch (err) {
            console.error(`${test}: Test failed to execute - ${err}`)

            let result = {
                success: false,
                message: "Test failed to execute"
            }

            return res.json(result)
        }
    }

    if (token) {
        app.post("/examiner/test/*", async function (req, res, next) {
            let request_token = req.query.token

            if (!request_token || request_token != token)
                return next()

            return await examiner_test(req, res, next)
        })
    }
    else {
        app.post("/examiner/test/*", examiner_test)
    }
}
