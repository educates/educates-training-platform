import * as vscode from "vscode"

import execWithIndices = require("regexp-match-indices")

export interface ReplaceMatchingTextParams {
    file: string,
    match: string,
    replacement: string,
    start?: number,
    stop?: number,
    isRegex?: boolean,
    group?: number
}

export async function replaceMatchingText(params: ReplaceMatchingTextParams) {
    // Display the editor window for the target file.

    const doc = await vscode.workspace.openTextDocument(params.file)
    const editor = await vscode.window.showTextDocument(doc)

    // Bail out if there was no text to match provided.

    if (!params.match)
        return

    if (params.replacement === undefined)
        return

    // Find the matching text based on whether regex or exact match.

    const lines = editor.document.lineCount

    let line = 0

    let startLine = (params.start === undefined || params.start === null) ? 0 : params.start
    let stopLine = (params.stop === undefined || params.stop === null) ? lines : params.stop

    if (startLine < 0)
        startLine = lines + startLine

    if (startLine < 0)
        startLine = 0
    else if (startLine >= lines)
        startLine = lines - 1

    if (stopLine < 0)
        stopLine = lines + stopLine

    if (stopLine < 0)
        stopLine = 0
    else if (stopLine >= lines)
        stopLine = lines - 1

    let startMatch = -1
    let stopMatch = -1

    if (params.isRegex) {
        let regex = new RegExp(params.match)
        let group = params.group || 0
        for (line = startLine; line < stopLine; line++) {
            let currentLine = editor.document.lineAt(line)
            let match = execWithIndices(regex, currentLine.text)
            if (match) {
                startMatch = match.indices[group][0]
                stopMatch = match.indices[group][1]
                break
            }
        }
    }
    else {
        for (line = startLine; line < stopLine; line++) {
            let currentLine = editor.document.lineAt(line)
            let offset = currentLine.text.indexOf(params.match)
            if (offset >= 0) {
                startMatch = offset
                stopMatch = offset + params.match.length
                break
            }
        }
    }

    // Bail out if there was no match found anywhere in the file.

    if (startMatch == -1)
        return

    // Select the matched text and replace it with the replacement text.

    let startPosition = new vscode.Position(line, startMatch)
    let stopPosition = new vscode.Position(line, stopMatch)
    let selection = new vscode.Selection(startPosition, stopPosition)
    editor.selection = selection

    await editor.edit(builder => builder.replace(editor.selection, params.replacement))

    editor.revealRange(editor.selection, vscode.TextEditorRevealType.InCenter)

    await editor.document.save()
}
