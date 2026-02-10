import * as fs from 'fs';
import * as vscode from 'vscode';

const log_file_path = "/tmp/educates-vscode-helper.log";

function log(message: string) {
    fs.appendFileSync(log_file_path, message + "\n");
}

// --- Terminal tracking ---

const terminals = new Map<string, vscode.Terminal>();

function findTerminal(name: string): vscode.Terminal | undefined {
    // Check our map first.
    let terminal = terminals.get(name);
    if (terminal) return terminal;

    // Search active terminals by name in case it was created externally.
    terminal = vscode.window.terminals.find(t => t.name === name);
    if (terminal) {
        terminals.set(name, terminal);
    }
    return terminal;
}

function findOrCreateTerminal(name: string): vscode.Terminal {
    let terminal = findTerminal(name);
    if (terminal) return terminal;

    terminal = vscode.window.createTerminal({ name });
    terminals.set(name, terminal);
    return terminal;
}

function sessionName(session: any): string {
    // Coerce to string since YAML may parse bare numbers (e.g. session: 1)
    // as integers rather than strings.
    if (session === undefined || session === null) return "educates";
    return String(session);
}

export function initTerminalOperations(): vscode.Disposable {
    return vscode.window.onDidCloseTerminal(terminal => {
        for (const [name, t] of terminals) {
            if (t === terminal) {
                terminals.delete(name);
                break;
            }
        }
    });
}

// --- Handler: terminal-open ---

export interface TerminalOpenParams {
    session?: any;
}

export async function handleTerminalOpen(params: TerminalOpenParams) {
    const name = sessionName(params.session);

    log('Requesting to open terminal:');
    log(`  session = ${name}`);

    const terminal = findOrCreateTerminal(name);
    terminal.show();
}

// --- Handler: terminal-close ---

export interface TerminalCloseParams {
    session?: any;
}

export async function handleTerminalClose(params: TerminalCloseParams) {
    const name = sessionName(params.session);

    log('Requesting to close terminal:');
    log(`  session = ${name}`);

    const terminal = findTerminal(name);
    if (terminal) {
        terminal.dispose();
    }
}

// --- Handler: terminal-send ---

export interface TerminalSendParams {
    session?: any;
    text: string;
    endl?: boolean;
}

export async function handleTerminalSend(params: TerminalSendParams) {
    const name = sessionName(params.session);
    const addNewLine = params.endl !== false;

    log('Requesting to send to terminal:');
    log(`  session = ${name}`);
    log(`  text = ${params.text}`);
    log(`  endl = ${addNewLine}`);

    const terminal = findOrCreateTerminal(name);
    terminal.show();
    terminal.sendText(params.text, addNewLine);
}

// --- Handler: terminal-interrupt ---

export interface TerminalInterruptParams {
    session?: any;
}

export async function handleTerminalInterrupt(params: TerminalInterruptParams) {
    const name = sessionName(params.session);

    log('Requesting to interrupt terminal:');
    log(`  session = ${name}`);

    const terminal = findTerminal(name);
    if (terminal) {
        terminal.show();
        terminal.sendText('\x03', false);
    }
}

// --- Handler: terminal-clear ---

export interface TerminalClearParams {
    session?: any;
}

export async function handleTerminalClear(params: TerminalClearParams) {
    const name = sessionName(params.session);

    log('Requesting to clear terminal:');
    log(`  session = ${name}`);

    const terminal = findTerminal(name);
    if (terminal) {
        terminal.show();
        await vscode.commands.executeCommand('workbench.action.terminal.clear');
    }
}
