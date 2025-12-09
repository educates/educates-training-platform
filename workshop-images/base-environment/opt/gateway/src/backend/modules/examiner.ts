import * as express from "express"
import * as child_process from "child_process"
import * as path from "path"
import * as fs from "fs"
import * as os from "os"

import { config } from "./config"

const test_program_directories = [
    "/home/eduk8s/workshop/examiner/tests",
    "/opt/workshop/examiner/tests",
    "/opt/eduk8s/workshop/examiner/tests",
    "/opt/renderer/workshop/examiner/tests"
];

/**
 * Validates that a test name contains only safe characters.
 * Allows alphanumeric characters, hyphens, underscores, and dots.
 * Prevents path traversal and command injection attempts.
 */
function validate_test_name(name: string): boolean {
    if (!name || typeof name !== 'string') {
        return false;
    }
    // Only allow alphanumeric, hyphens, underscores, and dots
    // Explicitly disallow path separators and other special characters
    const safePattern = /^[a-zA-Z0-9._-]+$/;
    return safePattern.test(name) && !name.includes('..') && !name.includes('/') && !name.includes('\\');
}

/**
 * Validates that a resolved pathname is within one of the allowed directories.
 * Prevents directory traversal attacks even if path.join() is bypassed.
 */
function validate_pathname(pathname: string): boolean {
    if (!pathname || typeof pathname !== 'string') {
        return false;
    }
    // Resolve to absolute path to prevent symlink attacks
    const resolvedPath = path.resolve(pathname);
    
    // Check if the resolved path is within any allowed directory
    for (const allowedDir of test_program_directories) {
        const resolvedDir = path.resolve(allowedDir);
        // Check if resolvedPath starts with resolvedDir followed by path.sep
        // This ensures the file is actually within the directory, not just a prefix match
        if (resolvedPath.startsWith(resolvedDir + path.sep) || resolvedPath === resolvedDir) {
            return true;
        }
    }
    return false;
}

/**
 * Validates command arguments to prevent injection attacks.
 * Ensures args is an array of strings without dangerous characters.
 */
function validate_args(args: any): string[] | null {
    if (!Array.isArray(args)) {
        return null;
    }
    
    const validatedArgs: string[] = [];
    for (const arg of args) {
        // Only allow string arguments
        if (typeof arg !== 'string') {
            return null;
        }
        // Reject empty strings and strings with null bytes (common injection vector)
        if (arg.length === 0 || arg.includes('\0')) {
            return null;
        }
        // Allow all printable characters in arguments (they're passed as array, not shell)
        // The spawn() function with array arguments doesn't use shell, so we just need
        // to ensure they're valid strings without null bytes
        validatedArgs.push(arg);
    }
    
    return validatedArgs;
}

function find_test_program(name: string): string | null {
    // Validate test name first
    if (!validate_test_name(name)) {
        return null;
    }

    let i: any;

    for (i in test_program_directories) {
        let pathname = path.join(test_program_directories[i], name);

        try {
            fs.accessSync(pathname, fs.constants.R_OK | fs.constants.X_OK);
            
            // Additional validation: ensure the resolved path is within allowed directory
            if (validate_pathname(pathname)) {
                return pathname;
            }
        } catch (err) {
            // Ignore it.
        }
    }
    
    return null;
}

export function setup_examiner(app: express.Application, token: string = null) {
    if (!config.enable_examiner)
        return

    app.use("/examiner/test/", express.json());

    async function examiner_test(req, res, next) {
        let test = req.params.test

        let options = req.body

        // Validate test parameter
        if (!test || !validate_test_name(test)) {
            return res.status(400).json({
                success: false,
                message: "Invalid test name"
            })
        }

        // Validate and sanitize arguments
        let rawArgs = options.args || []
        let args = validate_args(rawArgs)
        
        if (args === null) {
            return res.status(400).json({
                success: false,
                message: "Invalid arguments: must be an array of non-empty strings"
            })
        }

        let timeout = options.timeout || 15
        let form = options.form || {}

        let pathname = find_test_program(test)

        if (!pathname) {
            return res.sendStatus(404)
        }

        // Double-check pathname validation (defense in depth)
        if (!validate_pathname(pathname)) {
            console.error(`${test}: Security validation failed for pathname: ${pathname}`)
            return res.status(403).json({
                success: false,
                message: "Security validation failed"
            })
        }

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
                    logStream.write(logData+'\n'); // Append stdout to the log file.
                });
            });

            process.stderr.on('data', (data) => {
                const lines = data.toString().split('\n');
                lines.forEach((line) => {
                    const logData = `${test}: ${line}`;
                    console.log(logData);
                    logStream.write(logData+'\n'); // Append stderr to the log file.
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
        app.post("/examiner/test/:test", async function (req, res, next) {
            let request_token = req.query.token

            if (!request_token || request_token != token)
                return next()

            return await examiner_test(req, res, next)
        })
    }
    else {
        app.post("/examiner/test/:test", examiner_test)
    }
}
