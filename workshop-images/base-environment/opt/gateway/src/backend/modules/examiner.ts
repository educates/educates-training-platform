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
]

/**
 * Validates that a test name contains only safe characters.
 * Allows alphanumeric characters, hyphens, underscores, and dots.
 * Prevents path traversal and command injection attempts.
 */
function validate_test_name(name: string): boolean {
    if (!name || typeof name !== "string") {
        return false
    }

    // Only allow alphanumeric, hyphens, underscores, and dots
    const safePattern = /^[a-zA-Z0-9._-]+$/
    return (
        safePattern.test(name) &&
        !name.includes("..") &&
        !name.includes("/") &&
        !name.includes("\\")
    )
}

/**
 * Validates that a resolved pathname is within one of the allowed directories.
 * Prevents directory traversal and symlink attacks.
 * NOTE: This function now expects the input to be the canonical real path.
 */
function validate_pathname(pathname: string): boolean {
    if (!pathname || typeof pathname !== "string") {
        return false
    }

    // Use path.resolve here for consistency, but the input is expected to be canonical (no symlinks/..)
    const resolvedPath = path.resolve(pathname)

    for (const allowedDir of test_program_directories) {
        const resolvedDir = path.resolve(allowedDir)

        // Check if resolvedPath is the directory itself OR starts with the directory followed by path.sep
        if (
            resolvedPath === resolvedDir ||
            resolvedPath.startsWith(resolvedDir + path.sep)
        ) {
            return true
        }
    }

    return false
}

/**
 * Validates command arguments.
 * Ensures args is an array of non-empty strings without null bytes.
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

        // Reject empty strings and strings with null bytes (injection vector)
        if (arg.length === 0 || arg.includes("\0")) {
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
        return null
    }

    for (const dir of test_program_directories) {
        const potentialPath = path.join(dir, name)

        try {
            // 1. Check for file existence and executability
            fs.accessSync(potentialPath, fs.constants.R_OK | fs.constants.X_OK)

            // CRITICAL FIX: Resolve the path to its canonical form (no symlinks, no '..')
            const canonicalPath = fs.realpathSync(potentialPath)

            // 2. Validate the canonical path to ensure it is within boundaries
            if (!validate_pathname(canonicalPath)) {
                // This means the file exists, but it resolves to a path outside the allowed directories (e.g., symlink attack)
                console.error(`Security alert: Path ${potentialPath} resolved to unauthorized canonical path ${canonicalPath}`)
                continue
            }

            // Return the canonical path for safe execution
            return canonicalPath
        } catch (err) {
            // File doesn't exist or isn't accessible/resolvable, continue searching
        }
    }

    return null
}

export function setup_examiner(app: express.Application, token: string = null) {
    if (!config.enable_examiner) return

    app.use("/examiner/test/", express.json())

    async function examiner_test(req, res, next) {
        const test = req.params.test
        const options = req.body || {}

        // Validate test parameter early
        if (!test || !validate_test_name(test)) {
            return res.status(400).json({
                success: false,
                message: "Invalid test name"
            })
        }

        // Validate and sanitize args ONCE and reuse (Fixes Args Overwrite Issue)
        const rawArgs = options.args || []
        const args = validate_args(rawArgs)

        if (args === null) {
            return res.status(400).json({
                success: false,
                message: "Invalid arguments: must be an array of non-empty strings"
            })
        }

        const timeout = options.timeout || 15
        const form = options.form || {}

        // find_test_program returns the canonical path after strict validation
        const pathname = find_test_program(test)

        if (!pathname) {
            return res.sendStatus(404)
        }

        // Defense in depth: validate the final canonical pathname again
        // This is primarily a double-check, as find_test_program already did the heavy lifting.
        if (!validate_pathname(pathname)) {
            console.error(`${test}: Final security validation failed for pathname: ${pathname}`)
            return res.status(403).json({
                success: false,
                message: "Security validation failed"
            })
        }

        let process: any
        let timer: any

        try {
            // CRITICAL: Use shell-less spawn with validated path and arguments array
            process = child_process.spawn(pathname, args, {
                cwd: os.homedir()
            })

            process.on("error", (err) => {
                console.error(`${test}: Test failed to execute - ${err}`)
                return res.json({
                    success: false,
                    message: "Test failed to execute"
                })
            })

            process.on("exit", (code) => {
                if (timer) clearTimeout(timer)

                let result: any = {
                    success: code === 0,
                    message:
                        code === 0
                            ? "Test successfully completed"
                            : code === null
                                ? "Process killed or crashed"
                                : "Test failed to complete"
                }

                return res.json(result)
            })

            process.on("spawn", () => {
                if (form) {
                    process.stdin.setEncoding("utf-8")
                    process.stdin.on("error", (error) =>
                        console.log(`${test}: Error writing to stdin - ${error}`)
                    )
                    process.stdin.write(JSON.stringify(form))
                }

                process.stdin.end()
            })

            // Capture output to log file
            const logFilePath = path.join(
                os.homedir(),
                ".local/share/workshop/examiner-scripts.log"
            )
            const logStream = fs.createWriteStream(logFilePath, { flags: "a" })

            logStream.on("error", () => {
                // Ignore logging errors
            })

            process.stdout.on("data", (data) => {
                data
                    .toString()
                    .split("\n")
                    .forEach((line) => {
                        if (line) {
                            const logData = `${test}: ${line}`
                            console.log(logData)
                            logStream.write(logData + "\n")
                        }
                    })
            })

            process.stderr.on("data", (data) => {
                data
                    .toString()
                    .split("\n")
                    .forEach((line) => {
                        if (line) {
                            const logData = `${test}: ${line}`
                            console.log(logData)
                            logStream.write(logData + "\n")
                        }
                    })
            })

            if (timeout) {
                timer = setTimeout(() => {
                    console.error(`${test}: Test timeout expired`)
                    process.kill()
                }, timeout * 1000)
            }
        } catch (err) {
            console.error(`${test}: Test failed to execute - ${err}`)
            return res.json({
                success: false,
                message: "Test failed to execute"
            })
        }
    }

    if (token) {
        app.post("/examiner/test/:test", async (req, res, next) => {
            const request_token = req.query.token
            if (!request_token || request_token !== token) return next()
            return examiner_test(req, res, next)
        })
    } else {
        app.post("/examiner/test/:test", examiner_test)
    }
}