import * as vscode from "vscode"

export interface ReplaceTextSelectionParams {
    file: string,
    text: string
}

export async function replaceTextSelection(params: ReplaceTextSelectionParams) {
    // Display the editor window for the target file.

    const doc = await vscode.workspace.openTextDocument(params.file)
    const editor = await vscode.window.showTextDocument(doc)

    // Bail out if there was no text to match provided.

    if (params.text === undefined)
        return

    // Capture the selection start before the edit replaces it.

    const selStart = editor.selection.start

    await editor.edit(builder => builder.replace(editor.selection, params.text))

    // Select the replacement text so it is highlighted in the editor.

    const replacementLines = params.text.split('\n')
    let endLine: number, endCol: number
    if (replacementLines.length === 1) {
        endLine = selStart.line
        endCol = selStart.character + replacementLines[0].length
    } else {
        endLine = selStart.line + replacementLines.length - 1
        endCol = replacementLines[replacementLines.length - 1].length
    }
    editor.selection = new vscode.Selection(selStart, new vscode.Position(endLine, endCol))
    editor.revealRange(editor.selection, vscode.TextEditorRevealType.InCenter)

    await editor.document.save()
}
