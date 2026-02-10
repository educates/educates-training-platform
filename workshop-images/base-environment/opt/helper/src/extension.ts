import * as bodyParser from 'body-parser';
import { Request, Response } from 'express-serve-static-core';
import * as fs from 'fs';
import * as vscode from 'vscode';
import express = require('express');

import * as yaml from 'yaml';
import { Node, Pair, YAMLMap, YAMLSeq } from 'yaml/types';

import { ReplaceTextSelectionParams, replaceTextSelection } from './replace-text-selection';
import { SelectMatchingTextParams, selectMatchingText } from './select-matching-text';
import { ReplaceMatchingTextParams, replaceMatchingText } from './replace-matching-text';

const log_file_path = "/tmp/educates-vscode-helper.log";

function log(message: string) {
    fs.appendFileSync(log_file_path, message + "\n");
}

log('Loading educates-vscode-helper');

// --- Utility functions ---

function showEditor(file: string): Thenable<vscode.TextEditor> {
    return vscode.workspace.openTextDocument(file)
        .then(doc => {
            log("Opened document");
            return vscode.window.showTextDocument(doc);
        });
}

function insertTextAtLine(editor: vscode.TextEditor, line: number, text: string): Thenable<any> {
    log(`called insertTextAtLine(${line})`);
    let lines = editor.document.lineCount;
    while (lines <= line) {
        lines++;
        text = "\n" + text;
    }
    return editor.edit(editBuilder => {
        const loc = new vscode.Position(line, 0);
        editBuilder.insert(loc, text);
    })
        .then(() => revealLine(editor, line));
}

function revealLine(editor: vscode.TextEditor, line: number, before: number = 0, after: number = 0): void {
    log(`called revealLine(${line})`);
    let lineStart = new vscode.Position(line - before, 0);
    let lineEnd = new vscode.Position(line + after, 0);
    let sel = new vscode.Selection(lineStart, lineEnd);
    editor.selection = sel;
    editor.revealRange(editor.selection, vscode.TextEditorRevealType.InCenter);
}

function findLineContaining(editor: vscode.TextEditor, text: string, isRegex: boolean = false): number {
    if (!isRegex)
        text = text.trim();

    let regex = new RegExp(text);

    const lines = editor.document.lineCount;
    for (let line = 0; line < lines; line++) {
        let currentLine = editor.document.lineAt(line);
        if (isRegex) {
            if (currentLine.text.search(regex) >= 0)
                return line;
        }
        else if (currentLine.text.includes(text))
            return line;
    }
    return lines - 1;
}

function fileExists(file: string): Promise<boolean> {
    return new Promise((resolve) => {
        fs.access(file, fs.constants.F_OK, (err) => {
            resolve(err ? false : true);
        });
    });
}

function writeFile(file: string, content: string): Promise<void> {
    return new Promise<void>((resolve, reject) => {
        fs.writeFile(file, content, (err) => {
            if (err) {
                reject(err);
            } else {
                resolve();
            }
        });
    });
}

function ensureNewlineTerminated(text: string): string {
    if (!text.endsWith("\n")) {
        log("Adding missing newline terminator to text");
        text += "\n";
    }
    return text;
}

// --- Handler: open-file ---

interface OpenFileParams {
    file: string;
    line?: number;
}

async function handleOpenFile(params: OpenFileParams) {
    if (typeof params.line === 'number' && params.line > 0) {
        params.line--;
    }
    log('Requesting to open:');
    log(`  file = ${params.file}`);
    log(`  line = ${params.line}`);
    const editor = await showEditor(params.file);
    log("Showed document");
    if (typeof params.line === 'number' && params.line >= 0) {
        revealLine(editor, params.line);
    }
}

// --- Handler: create-file ---

interface CreateFileParams {
    file: string;
    text: string;
}

async function handleCreateFile(params: CreateFileParams) {
    log('Requesting to create file:');
    log(`  file = ${params.file}`);

    let content = params.text || "";

    if (content && !content.endsWith("\n")) {
        content += "\n";
    }

    if (await fileExists(params.file)) {
        // File exists — open it, select all content, replace with new content.
        const editor = await showEditor(params.file);
        const doc = editor.document;
        const entireRange = new vscode.Range(
            new vscode.Position(0, 0),
            new vscode.Position(doc.lineCount, 0)
        );
        await editor.edit(editBuilder => {
            editBuilder.replace(entireRange, content);
        });
        await doc.save();
        // Clear any selection so it doesn't carry over to new content.
        const origin = new vscode.Position(0, 0);
        editor.selection = new vscode.Selection(origin, origin);
    } else {
        // File does not exist — create it with the content.
        await writeFile(params.file, content);
        const editor = await showEditor(params.file);
        // Clear any selection carried over from a previous editor tab.
        const origin = new vscode.Position(0, 0);
        editor.selection = new vscode.Selection(origin, origin);
    }
}

// --- Handler: append-to-file ---

interface AppendToFileParams {
    file: string;
    text: string;
}

async function handleAppendToFile(params: AppendToFileParams) {
    let text = ensureNewlineTerminated(params.text || "");

    log('Requesting to append to file:');
    log(`  file = ${params.file}`);
    log(`  text = ${text}`);

    if (await fileExists(params.file)) {
        const editor = await showEditor(params.file);

        // Handle special case when last line of the document is empty.
        let lines = editor.document.lineCount;
        const lastLine = editor.document.getText(new vscode.Range(
            new vscode.Position(lines - 1, 0),
            new vscode.Position(lines, 0)
        ));
        if (!lastLine) {
            lines--;
        }
        await insertTextAtLine(editor, lines, text);
        await editor.document.save();
    } else {
        await writeFile(params.file, text);
        await showEditor(params.file);
    }
}

// --- Handler: insert-before-line ---

interface InsertBeforeLineParams {
    file: string;
    line: number;
    text: string;
}

async function handleInsertBeforeLine(params: InsertBeforeLineParams) {
    let line = params.line;
    if (typeof line === 'number') {
        line--;
    }

    let text = ensureNewlineTerminated(params.text || "");

    log('Requesting to insert before line:');
    log(`  file = ${params.file}`);
    log(`  line = ${line}`);
    log(`  text = ${text}`);

    if (await fileExists(params.file)) {
        const editor = await showEditor(params.file);
        if (typeof line === 'number' && line >= 0) {
            await insertTextAtLine(editor, line, text);
        }
        await editor.document.save();
    } else {
        await writeFile(params.file, text);
        await showEditor(params.file);
    }
}

// --- Handler: insert-after-line ---

interface InsertAfterLineParams {
    file: string;
    line: number;
    text: string;
}

async function handleInsertAfterLine(params: InsertAfterLineParams) {
    // Insert after line N means insert before line N+1.
    // The line number from the frontend is 1-based, so after converting to
    // 0-based by subtracting 1, we add 1 to get the position after.
    // Net effect: use the line number as-is (it's already the 0-based position after).

    let text = ensureNewlineTerminated(params.text || "");

    log('Requesting to insert after line:');
    log(`  file = ${params.file}`);
    log(`  line = ${params.line}`);
    log(`  text = ${text}`);

    if (await fileExists(params.file)) {
        const editor = await showEditor(params.file);
        if (typeof params.line === 'number' && params.line >= 0) {
            await insertTextAtLine(editor, params.line, text);
        }
        await editor.document.save();
    } else {
        await writeFile(params.file, text);
        await showEditor(params.file);
    }
}

// --- Handler: delete-lines ---

interface DeleteLinesParams {
    file: string;
    start: number;
    stop?: number;
}

async function handleDeleteLines(params: DeleteLinesParams) {
    log('Requesting to delete lines:');
    log(`  file = ${params.file}`);
    log(`  start = ${params.start}`);
    log(`  stop = ${params.stop}`);

    const editor = await showEditor(params.file);
    const doc = editor.document;
    const lines = doc.lineCount;

    // Convert 1-based line numbers to 0-based.
    let startLine = params.start - 1;
    let stopLine = (params.stop === undefined || params.stop === null) ? startLine : params.stop - 1;

    // Clamp to valid range.
    if (startLine < 0)
        startLine = 0;
    if (startLine >= lines)
        startLine = lines - 1;
    if (stopLine < 0)
        stopLine = 0;
    if (stopLine >= lines)
        stopLine = lines - 1;

    // Ensure start <= stop.
    if (startLine > stopLine) {
        let temp = startLine;
        startLine = stopLine;
        stopLine = temp;
    }

    // Delete from start of startLine to start of line after stopLine.
    let startPosition = new vscode.Position(startLine, 0);
    let endPosition: vscode.Position;

    if (stopLine + 1 < lines) {
        endPosition = new vscode.Position(stopLine + 1, 0);
    } else {
        // Deleting to end of file — include from end of previous line if possible.
        let lastLine = doc.lineAt(stopLine);
        endPosition = new vscode.Position(stopLine, lastLine.text.length);
        if (startLine > 0) {
            let prevLine = doc.lineAt(startLine - 1);
            startPosition = new vscode.Position(startLine - 1, prevLine.text.length);
        }
    }

    let range = new vscode.Range(startPosition, endPosition);

    await editor.edit(editBuilder => {
        editBuilder.delete(range);
    });

    editor.revealRange(new vscode.Range(startPosition, startPosition), vscode.TextEditorRevealType.InCenter);

    await doc.save();
}

// --- Handler: delete-matching-lines ---

interface DeleteMatchingLinesParams {
    file: string;
    match: string;
    isRegex?: boolean;
    count?: number;
    before?: number;
    after?: number;
}

async function handleDeleteMatchingLines(params: DeleteMatchingLinesParams) {
    log('Requesting to delete matching lines:');
    log(`  file = ${params.file}`);
    log(`  match = ${params.match}`);
    log(`  isRegex = ${params.isRegex}`);
    log(`  count = ${params.count}`);
    log(`  before = ${params.before}`);
    log(`  after = ${params.after}`);

    if (!params.match)
        return;

    const editor = await showEditor(params.file);
    const doc = editor.document;
    const lines = doc.lineCount;

    // Find the matching line.
    let matchLine = -1;

    if (params.isRegex) {
        let regex = new RegExp(params.match);
        for (let line = 0; line < lines; line++) {
            if (regex.test(doc.lineAt(line).text)) {
                matchLine = line;
                break;
            }
        }
    } else {
        let text = params.match.trim();
        for (let line = 0; line < lines; line++) {
            if (doc.lineAt(line).text.includes(text)) {
                matchLine = line;
                break;
            }
        }
    }

    if (matchLine === -1)
        return;

    // Calculate the range of lines to delete.
    let linesBefore = (params.before === undefined || params.before === null) ? 0 : params.before;
    let linesAfter = (params.after === undefined || params.after === null) ? 0 : params.after;
    let count = (params.count === undefined || params.count === null) ? 1 : params.count;

    // Use -1 to indicate all lines before or after.
    if (linesBefore < 0)
        linesBefore = matchLine;
    if (linesAfter < 0)
        linesAfter = lines - matchLine - 1;

    // Count is how many lines from match onwards (minimum 1).
    if (count < 0)
        count = lines - matchLine;
    if (count < 1)
        count = 1;

    let startLine = matchLine - linesBefore;
    let stopLine = matchLine + count - 1 + linesAfter;

    if (startLine < 0)
        startLine = 0;
    if (stopLine >= lines)
        stopLine = lines - 1;

    // Delete the range.
    let startPosition = new vscode.Position(startLine, 0);
    let endPosition: vscode.Position;

    if (stopLine + 1 < lines) {
        endPosition = new vscode.Position(stopLine + 1, 0);
    } else {
        let lastLine = doc.lineAt(stopLine);
        endPosition = new vscode.Position(stopLine, lastLine.text.length);
        if (startLine > 0) {
            let prevLine = doc.lineAt(startLine - 1);
            startPosition = new vscode.Position(startLine - 1, prevLine.text.length);
        }
    }

    let range = new vscode.Range(startPosition, endPosition);

    await editor.edit(editBuilder => {
        editBuilder.delete(range);
    });

    editor.revealRange(new vscode.Range(startPosition, startPosition), vscode.TextEditorRevealType.InCenter);

    await doc.save();
}

// --- Handler: replace-lines ---

interface ReplaceLinesParams {
    file: string;
    start: number;
    stop: number;
    text: string;
}

async function handleReplaceLines(params: ReplaceLinesParams) {
    log('Requesting to replace lines:');
    log(`  file = ${params.file}`);
    log(`  start = ${params.start}`);
    log(`  stop = ${params.stop}`);

    const editor = await showEditor(params.file);
    const doc = editor.document;
    const lines = doc.lineCount;

    // Convert 1-based line numbers to 0-based.
    let startLine = params.start - 1;
    let stopLine = params.stop - 1;

    // Clamp to valid range.
    if (startLine < 0)
        startLine = 0;
    if (startLine >= lines)
        startLine = lines - 1;
    if (stopLine < 0)
        stopLine = 0;
    if (stopLine >= lines)
        stopLine = lines - 1;

    // Ensure start <= stop.
    if (startLine > stopLine) {
        let temp = startLine;
        startLine = stopLine;
        stopLine = temp;
    }

    let replacement = ensureNewlineTerminated(params.text || "");

    // Replace from start of startLine to start of line after stopLine.
    let startPosition = new vscode.Position(startLine, 0);
    let endPosition: vscode.Position;

    if (stopLine + 1 < lines) {
        endPosition = new vscode.Position(stopLine + 1, 0);
    } else {
        let lastLine = doc.lineAt(stopLine);
        endPosition = new vscode.Position(stopLine, lastLine.text.length);
        // When replacing the last lines, we need a leading newline instead of trailing.
        if (startLine > 0) {
            let prevLine = doc.lineAt(startLine - 1);
            startPosition = new vscode.Position(startLine - 1, prevLine.text.length);
            replacement = "\n" + replacement;
            if (replacement.endsWith("\n")) {
                replacement = replacement.slice(0, -1);
            }
        }
    }

    let range = new vscode.Range(startPosition, endPosition);

    await editor.edit(editBuilder => {
        editBuilder.replace(range, replacement);
    });

    editor.revealRange(new vscode.Range(startPosition, startPosition), vscode.TextEditorRevealType.InCenter);

    await doc.save();
}

// --- Handler: insert-after-match ---

interface InsertAfterMatchParams {
    file: string;
    match: string;
    text: string;
}

async function handleInsertAfterMatch(params: InsertAfterMatchParams) {
    let text = ensureNewlineTerminated(params.text || "");

    log('Requesting to insert after match:');
    log(`  file = ${params.file}`);
    log(`  match = ${params.match}`);
    log(`  text = ${text}`);

    if (await fileExists(params.file)) {
        const editor = await showEditor(params.file);
        const line = findLineContaining(editor, params.match);
        log("line = " + line);
        if (line >= 0) {
            await insertTextAtLine(editor, line + 1, text);
        }
        await editor.document.save();
    } else {
        await writeFile(params.file, text);
        await showEditor(params.file);
    }
}

// --- Handler: copy-file ---

interface CopyFileParams {
    src: string;
    dest: string;
    open?: boolean;
}

async function handleCopyFile(params: CopyFileParams) {
    log('Requesting to copy file:');
    log(`  src = ${params.src}`);
    log(`  dest = ${params.dest}`);

    const srcUri = vscode.Uri.file(params.src);
    const destUri = vscode.Uri.file(params.dest);

    await vscode.workspace.fs.copy(srcUri, destUri, { overwrite: true });

    if (params.open !== false) {
        await showEditor(params.dest);
    }
}

// --- Handler: rename-file ---

interface RenameFileParams {
    src: string;
    dest: string;
    open?: boolean;
}

async function handleRenameFile(params: RenameFileParams) {
    log('Requesting to rename file:');
    log(`  src = ${params.src}`);
    log(`  dest = ${params.dest}`);

    const srcUri = vscode.Uri.file(params.src);
    const destUri = vscode.Uri.file(params.dest);

    await vscode.workspace.fs.rename(srcUri, destUri, { overwrite: true });

    if (params.open !== false) {
        await showEditor(params.dest);
    }
}

// --- Handler: close-file ---

interface CloseFileParams {
    file: string;
}

async function handleCloseFile(params: CloseFileParams) {
    log('Requesting to close file:');
    log(`  file = ${params.file}`);

    const targetUri = vscode.Uri.file(params.file);

    // Find the tab for this file and close it.
    for (const group of vscode.window.tabGroups.all) {
        for (const tab of group.tabs) {
            if (tab.input instanceof vscode.TabInputText) {
                if (tab.input.uri.fsPath === targetUri.fsPath) {
                    await vscode.window.tabGroups.close(tab);
                    return;
                }
            }
        }
    }
}

// --- Handler: delete-file ---

interface DeleteFileParams {
    file: string;
}

async function handleDeleteFile(params: DeleteFileParams) {
    log('Requesting to delete file:');
    log(`  file = ${params.file}`);

    const fileUri = vscode.Uri.file(params.file);

    // Close the file first if it's open.
    await handleCloseFile({ file: params.file });

    // Delete the file.
    await vscode.workspace.fs.delete(fileUri);
}

// --- Handler: paste (YAML path insertion only — kept for insert-value-into-yaml) ---

interface PasteAtYamlPathParams {
    file: string;
    yamlPath: string;
    paste: string;
}

async function handlePasteAtYamlPath(params: PasteAtYamlPathParams) {
    let paste = ensureNewlineTerminated(params.paste);

    log('Requesting to paste at yaml path:');
    log(`  file = ${params.file}`);
    log(`  yamlPath = ${params.yamlPath}`);
    log(`  paste = ${paste}`);

    if (await fileExists(params.file)) {
        await pasteAtYamlPath(params.file, params.yamlPath, paste);
    } else {
        await writeFile(params.file, paste);
        await showEditor(params.file);
    }
}

async function pasteAtYamlPath(file: string, yamlPath: string, paste: string): Promise<any> {
    let editor = await showEditor(file);
    let text = editor.document.getText();
    let opts: yaml.Options = {
    };
    let doc = yaml.parseAllDocuments(text, opts)[0];
    let target = findNode(doc, yamlPath);
    log("Found target node with range: " + target?.range);
    let rng: [number, number] | null = null;
    if (target instanceof YAMLMap) {
        let lastChild = target.items[target.items.length - 1];
        rng = rangeOf(lastChild);
    } else if (target instanceof YAMLSeq) {
        rng = rangeOf(target);
    }
    if (rng) {
        let startPos = editor.document.positionAt(rng[0]);
        let end = rng[1];
        while (end > 0 && text[end - 1].trim() === '') {
            end--;
        }
        let endPos = editor.document.positionAt(end);
        let indent = " ".repeat(startPos.character);
        if (indent) {
            paste = indent + paste.trim().replace(new RegExp('\n', 'g'), '\n' + indent) + '\n';
        }
        await insertTextAtLine(editor, endPos.line + 1, paste);
        await editor.document.save();
    }
}

// --- YAML navigation utilities (kept for insert-value-into-yaml) ---

function rangeOf(item: any): [number, number] | null {
    if (item instanceof Pair) {
        let start = item?.key?.range?.[0];
        let end = item?.value?.range?.[1];
        if (typeof (start) === 'number' && typeof (end) === 'number') {
            return [start, end];
        }
    } else if (item instanceof Node) {
        return item.range || null;
    }
    return null;
}

function findNode(doc: yaml.Document.Parsed, path: string): Node | null {
    if (doc.contents) {
        return navigate(doc.contents, path);
    }
    return null;
}

function navigate(node: Node, path: string): Node {
    if (!path) {
        return node;
    } else {
        let head = parsePath(path);
        if (head.key) {
            if (node.type === 'MAP') {
                let map = node as YAMLMap;
                let val = map.get(head.key);
                if (val) {
                    return navigate(val, head.tail);
                }
            }
            throw new Error("Key not found: " + head.key);
        }
        if (typeof (head.index) === 'number') {
            if (node.type === 'SEQ') {
                let seq = node as YAMLSeq;
                let val = seq.get(head.index);
                return navigate(val, head.tail);
            }
            throw new Error("Index not found: " + head.index);
        }
        if (head.attribute) {
            if (node.type === 'SEQ') {
                let seq = node as YAMLSeq;
                const items = seq.items.length;
                for (let index = 0; index < items; index++) {
                    const child: Node = seq.get(index);
                    if (child instanceof YAMLMap) {
                        let val = child.get(head.attribute.key);
                        if (val === head.attribute.value) {
                            return navigate(child, head.tail);
                        }
                    }
                }
            }
            throw new Error(`Attribute not found ${head.attribute.key}=${head.attribute.value}`);
        }
    }
    throw new Error("Invalid yaml path");
}

interface Attribute {
    key: string,
    value: string
}

interface Path {
    key?: string,
    index?: number,
    attribute?: Attribute,
    tail: string
}

const dotOrBracket = /\.|\[/;

function parsePath(path: string): Path {
    if (path[0] === '[') {
        let closeBracket = path.indexOf(']');
        let tail = path.substring(closeBracket + 1);
        if (closeBracket >= 0) {
            const bracketed = path.substring(1, closeBracket);
            const eq = bracketed.indexOf('=');
            if (eq >= 0) {
                return {
                    attribute: {
                        key: bracketed.substring(0, eq),
                        value: bracketed.substring(eq + 1)
                    },
                    tail
                };
            } else {
                return {
                    index: parseInt(path.substring(1, closeBracket)),
                    tail
                };
            }
        }
    } else if (path[0] === '.') {
        return parsePath(path.substring(1));
    } else {
        let sep = path.search(dotOrBracket);
        if (sep >= 0) {
            if (path[sep] === '.') {
                return {
                    key: path.substring(0, sep),
                    tail: path.substring(sep + 1)
                };
            } else {
                return {
                    key: path.substring(0, sep),
                    tail: path.substring(sep)
                }
            }
        } else {
            return {
                key: path,
                tail: ""
            };
        }
    }
    throw new Error("invalid yaml path syntax");
}

// --- HTTP response helper ---

function createResponse(result: Promise<any>, req: Request<any>, res: Response<any>) {
    res.setHeader('Content-Type', 'text/plain');
    result.then(() => {
        log("Sending http ok response");
        res.send('OK\n');
    },
        (error) => {
            log(`Error handling request for '${req.url}':  ${error}`);
            log("Sending http ERROR response");
            res.status(500).send('FAIL\n');
        });
}

// --- Extension activation ---

export function activate(context: vscode.ExtensionContext) {

    log('Activating Educates helper');

    const port = process.env.EDUCATES_VSCODE_HELPER_PORT || 10011;

    const app: express.Application = express();

    app.use(bodyParser.json());

    // Health check endpoint.

    app.get("/hello", (req, res) => {
        res.send('Educates VS Code helper is running.\n');
    });

    // VS Code command execution endpoint (excluded from refactoring).

    let commandInProgress = false;
    app.post('/command/:id', (req, res) => {
        res.setHeader('Content-Type', 'text/plain');
        if (commandInProgress) {
            res.status(200).send("SKIPPED");
        } else {
            commandInProgress = true;
            const parameters: any[] = Array.isArray(req.body) ? req.body : [];
            vscode.commands.executeCommand(req.params.id, ...parameters).then(
                () => {
                    log(`Successfully executed command: '${req.params.id}'`);
                    commandInProgress = false;
                },
                (error) => {
                    log(`Failed executing command '${req.params.id}': ${error}`);
                    commandInProgress = false;
                }
            );
            res.status(202).send();
        }
    });

    // Editor endpoints — each has a clear, single purpose.

    app.post('/editor/open-file', (req, res) => {
        const parameters = req.body as OpenFileParams;
        createResponse(handleOpenFile(parameters), req, res);
    });

    app.post('/editor/create-file', (req, res) => {
        const parameters = req.body as CreateFileParams;
        createResponse(handleCreateFile(parameters), req, res);
    });

    app.post('/editor/append-to-file', (req, res) => {
        const parameters = req.body as AppendToFileParams;
        createResponse(handleAppendToFile(parameters), req, res);
    });

    app.post('/editor/insert-before-line', (req, res) => {
        const parameters = req.body as InsertBeforeLineParams;
        createResponse(handleInsertBeforeLine(parameters), req, res);
    });

    app.post('/editor/insert-after-line', (req, res) => {
        const parameters = req.body as InsertAfterLineParams;
        createResponse(handleInsertAfterLine(parameters), req, res);
    });

    app.post('/editor/insert-after-match', (req, res) => {
        const parameters = req.body as InsertAfterMatchParams;
        createResponse(handleInsertAfterMatch(parameters), req, res);
    });

    app.post('/editor/select-matching-text', (req, res) => {
        const parameters = req.body as SelectMatchingTextParams;
        createResponse(selectMatchingText(parameters), req, res);
    });

    app.post('/editor/replace-text-selection', (req, res) => {
        const parameters = req.body as ReplaceTextSelectionParams;
        createResponse(replaceTextSelection(parameters), req, res);
    });

    app.post('/editor/replace-matching-text', (req, res) => {
        const parameters = req.body as ReplaceMatchingTextParams;
        createResponse(replaceMatchingText(parameters), req, res);
    });

    app.post('/editor/delete-lines', (req, res) => {
        const parameters = req.body as DeleteLinesParams;
        createResponse(handleDeleteLines(parameters), req, res);
    });

    app.post('/editor/delete-matching-lines', (req, res) => {
        const parameters = req.body as DeleteMatchingLinesParams;
        createResponse(handleDeleteMatchingLines(parameters), req, res);
    });

    app.post('/editor/replace-lines', (req, res) => {
        const parameters = req.body as ReplaceLinesParams;
        createResponse(handleReplaceLines(parameters), req, res);
    });

    app.post('/editor/copy-file', (req, res) => {
        const parameters = req.body as CopyFileParams;
        createResponse(handleCopyFile(parameters), req, res);
    });

    app.post('/editor/rename-file', (req, res) => {
        const parameters = req.body as RenameFileParams;
        createResponse(handleRenameFile(parameters), req, res);
    });

    app.post('/editor/close-file', (req, res) => {
        const parameters = req.body as CloseFileParams;
        createResponse(handleCloseFile(parameters), req, res);
    });

    app.post('/editor/delete-file', (req, res) => {
        const parameters = req.body as DeleteFileParams;
        createResponse(handleDeleteFile(parameters), req, res);
    });

    // YAML path insertion — kept on /editor/paste for now (excluded from refactoring).

    app.post('/editor/paste', (req, res) => {
        const parameters = req.body as PasteAtYamlPathParams;
        createResponse(handlePasteAtYamlPath(parameters), req, res);
    });

    let server = app.listen(port, () => {
        log(`Educates helper is listening on port ${port}`);
    });

    server.on('error', e => {
        log('Problem starting server. Port in use?');
    });

    context.subscriptions.push({ dispose: () => server.close() });
}

export function deactivate() { }
