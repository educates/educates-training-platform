// The module 'vscode' contains the VS Code extensibility API
// Import the module and reference it with the alias vscode in your code below
import * as bodyParser from 'body-parser';
import { Request, Response } from 'express-serve-static-core';
import * as fs from 'fs';
import * as vscode from 'vscode';
import express = require('express');

import * as yaml from 'yaml';
import { Node, Pair, YAMLMap, YAMLSeq } from 'yaml/types';

import { ReplaceTextSelectionParams, replaceTextSelection } from './replace-text-selection';
import { SelectMatchingTextParams, selectMatchingText } from './select-matching-text';

const log_file_path = "/tmp/educates-vscode-helper.log";

function log(message: string) {
    fs.appendFileSync(log_file_path, message + "\n");
}

log('Loading educates-vscode-helper');

function showEditor(file: string): Thenable<vscode.TextEditor> {
    return vscode.workspace.openTextDocument(file)
        .then(doc => {
            log("Opened document");
            //TODO: select line? How?
            return vscode.window.showTextDocument(doc);
        });
}

function pasteAtLine(editor: vscode.TextEditor, line: number, paste: string): Thenable<any> {
    log(`called pasteAtLine(${line})`);
    let lines = editor.document.lineCount;
    while (lines <= line) {
        lines++;
        paste = "\n" + paste;
    }
    return editor.edit(editBuilder => {
        const loc = new vscode.Position(line, 0);
        editBuilder.insert(loc, paste);
    })
        .then(editApplies => gotoLine(editor, line));
}

function gotoLine(editor: vscode.TextEditor, line: number, before: number = 0, after: number = 0): void {
    log(`called gotoLine(${line})`);
    let lineStart = new vscode.Position(line - before, 0);
    let lineEnd = new vscode.Position(line + after, 0);
    let sel = new vscode.Selection(lineStart, lineEnd);
    log("Setting selection");
    editor.selection = sel;
    log("Revealing selection");
    editor.revealRange(editor.selection, vscode.TextEditorRevealType.InCenter);
}

function findLine(editor: vscode.TextEditor, text: string, isRegex: boolean = false): number {
    if (!isRegex)
        text = text.trim(); //ignore trailing and leading whitespace

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
    return lines - 1; // pretend we found the snippet on the last line. 
    // pasting there is probably better than just dropping the paste text silently.
}

function exists(file: string): Promise<boolean> {
    return new Promise((resolve, reject) => {
        fs.access(file, fs.constants.F_OK, (err) => {
            resolve(err ? false : true);
        });
    });
}

function createFile(file: string, content: string): Promise<any> {
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

async function pasteAtYamlPath(file: string, yamlPath: string, paste: string): Promise<any> {
    let editor = await showEditor(file);
    let text = editor.document.getText();
    let opts: yaml.Options = {
    };
    //TODO: deal with multi docs properly. For now we just assume one document.
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
        //find the real end not (i.e not including whitespace)
        while (end > 0 && text[end - 1].trim() === '') {
            end--;
        }
        let endPos = editor.document.positionAt(end);
        let indent = " ".repeat(startPos.character);
        if (indent) {
            paste = indent + paste.trim().replace(new RegExp('\n', 'g'), '\n' + indent) + '\n';
        }
        return pasteAtLine(editor, endPos.line + 1, paste);
    }
}

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
        let tp = node.type;
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
        //careful, head.index may be 0 so which is falsy
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
            } else { // path[sep]==='['
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

interface PasteParams {
    file: string;
    prefix?: string;
    line?: number;
    paste: string;
    yamlPath?: string;
}

async function handlePaste(params: PasteParams) {
    if (typeof params.line === 'number') {
        params.line--;
    }

    if (!params.paste.endsWith("\n")) {
        log("Adding missing newline terminator to paste string");
        params.paste += "\n";
    }

    log('Requesting to paste:');
    log(` file = ${params.file}`);
    log(`  pre = ${params.prefix}`);
    log(` line = ${params.line}`);
    log(`paste = ${params.paste}`);
    log(` yamlPath = ${params.yamlPath}`);

    if (await exists(params.file)) {
        log(`File '${params.file}' exists`);
        const editor = await showEditor(params.file);

        log("Editor shown");
        if (params.yamlPath) {
            log("Paste at yaml codepath");
            await pasteAtYamlPath(params.file, params.yamlPath, params.paste);
        } else if (typeof params.line === 'number' && params.line >= 0) {
            log("Paste at line codepath");
            await pasteAtLine(editor, params.line, params.paste);
        } else if (params.prefix) {
            log("Paste at prefix codepath");
            const line = findLine(editor, params.prefix);
            log("line = " + line);
            if (line >= 0) {
                //paste it on the *next* line after the found line
                await pasteAtLine(editor, line + 1, params.paste);
            }
        } else {
            //handle special case when last line of the document is empty
            let lines = editor.document.lineCount;
            const lastLine = editor.document.getText(new vscode.Range(
                new vscode.Position(lines - 1, 0),
                new vscode.Position(lines, 0)
            ));
            if (!lastLine) {
                lines--;
            }
            await pasteAtLine(editor, lines, params.paste);
        }

        await editor.document.save()
    } else {
        log("File does not exist");
        await createFile(params.file, params.paste);
        log("Created file");
        return await showEditor(params.file);
    }
}

interface GoToLineParams {
    file: string;
    line?: number;
}

async function handleGoToLine(params: GoToLineParams) {
    if (typeof params.line === 'number' && params.line > 0) {
        params.line--;
    }
    log('Requesting to open:');
    log(`  file = ${params.file}`);
    log(`  line = ${params.line}`);
    const editor = await showEditor(params.file);
    log("Showed document");
    if (typeof params.line === 'number' && params.line >= 0) {
        gotoLine(editor, params.line);
    }
}

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

// this method is called when your extension is activated
// your extension is activated the very first time the command is executed
export function activate(context: vscode.ExtensionContext) {

    log('Activating Educates helper');

    const port = process.env.EDUCATES_VSCODE_HELPER_PORT || 10011;

    const app: express.Application = express();

    app.use(bodyParser.json());

    app.get("/hello", (req, res) => {
        res.send('Hello World V5 with POST requests accepting JSON body!\n');
        const pre = req.query.pre;
    });

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

    app.post('/editor/paste', (req, res) => {
        const parameters = req.body || {} as PasteParams;
        createResponse(handlePaste(parameters), req, res);
    });

    app.get('/editor/paste', (req, res) => {
        const parameters: PasteParams = {
            file: req.query.file as string,
            prefix: req.query.prefix as string,
            line: req.query.line ? parseInt(req.query.line as string) : undefined,
            yamlPath: req.query.yamlPath as string,
            paste: req.query.paste as string
        };
        createResponse(handlePaste(parameters), req, res);
    });

    app.post('/editor/line', (req, res) => {
        const parameters = req.body as GoToLineParams;
        createResponse(handleGoToLine(parameters), req, res);
    });

    //TODO: change to a 'put' or 'update' request?
    app.get('/editor/line', (req, res) => {
        const file: string = req.query.file as string;
        const line = req.query.line ? parseInt(req.query.line as string) : undefined;

        createResponse(handleGoToLine({
            file,
            line
        }), req, res);
    });

    app.post('/editor/select-matching-text', (req, res) => {
        const parameters = req.body as SelectMatchingTextParams;
        createResponse(selectMatchingText(parameters), req, res);
    });

    app.post('/editor/replace-text-selection', (req, res) => {
        const parameters = req.body as ReplaceTextSelectionParams;
        createResponse(replaceTextSelection(parameters), req, res);
    });

    let server = app.listen(port, () => {
        log(`Educates helper is listening on port ${port}`);
    });

    server.on('error', e => {
        log('Problem starting server. Port in use?');
    });

    context.subscriptions.push({ dispose: () => server.close() });
}

// this method is called when your extension is deactivated
export function deactivate() { }
