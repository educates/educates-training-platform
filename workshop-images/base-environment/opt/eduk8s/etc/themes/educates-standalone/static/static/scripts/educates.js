const educates = (function () {
    // Function to copy text to clipboard
    function set_paste_buffer_to_text(text) {
        navigator.clipboard.writeText(text).catch(err => {
            console.error('Failed to copy text: ', err);
        });
    }

    // The Terminals class is a stub implementation which will be replaced
    // by the parent frame's terminals object if it exists. The stub will be
    // used when workshop pages are viewed as standalone pages.

    class Terminals {
        paste_to_terminal(text, session) {
            console.log('paste_to_terminal:', text, session);
        }

        paste_to_all_terminals(text) {
            console.log('paste_to_all_terminals:', text);
        }

        execute_in_terminal(command, session, clear) {
            console.log('execute_in_terminal:', command, session, clear);
        }

        execute_in_all_terminals(command, clear) {
            console.log('execute_in_all_terminals:', command, clear);
        }

        select_terminal(session) {
            console.log('select_terminal:', session);
            return true;
        }

        clear_terminal(session) {
            console.log('clear_terminal:', session);
        }

        clear_all_terminals() {
            console.log('clear_all_terminals');
        }

        interrupt_terminal(session) {
            console.log('interrupt_terminal:', session);
        }

        interrupt_all_terminals() {
            console.log('interrupt_all_terminals');
        }
    }

    var terminals = new Terminals();

    if (parent && parent.educates && parent.educates.terminals) {
        terminals = parent.educates.terminals;
    }

    // The Dashboard class is a stub implementation which will be replaced
    // by the parent frame's dashboard object if it exists. The stub will be
    // used when workshop pages are viewed as standalone pages.

    class Dashboard {
        session_owner() {
            console.log('session_owner');
            return "educates";
        }

        expose_terminal(session) {
            console.log('expose_terminal:', session);
            return true;
        }

        expose_dashboard(name) {
            console.log('expose_dashboard:', name);
            return true;
        }

        create_dashboard(name, url, focus) {
            console.log('create_dashboard:', name, url, focus);
            return true;
        }

        delete_dashboard(name) {
            console.log('delete_dashboard:', name);
            return true;
        }

        reload_dashboard(name, url, focus) {
            console.log('reload_dashboard:', name, url, focus);
            return true;
        }

        collapse_workshop() {
            console.log('collapse_workshop');
        }

        reload_workshop() {
            console.log('reload_workshop');
        }

        finished_workshop() {
            console.log('finished_workshop');
        }

        terminate_session() {
            console.log('terminate_session');
        }

        preview_image(src, title) {
            // Pop out images when clicked in a modal dialog.

            const preview_element = document.getElementById('preview-image-element');
            const preview_title = document.getElementById('preview-image-title');
            const preview_dialog = document.getElementById('preview-image-dialog');

            if (preview_element && preview_title && preview_dialog) {
                preview_element.setAttribute('src', src);
                preview_title.textContent = title;
                const modal = new bootstrap.Modal(preview_dialog);
                modal.show();
            }
        }
    }

    var dashboard = new Dashboard();

    if (parent && parent.educates && parent.educates.dashboard) {
        dashboard = parent.educates.dashboard;
    }


    document.addEventListener('DOMContentLoaded', function () {
        // Attach event listeners to all inline-copy elements.
    
        const elements = document.querySelectorAll('.inline-copy');

        elements.forEach(element => {
            // Find the preceding code element.

            const target = element.previousElementSibling;

            if (target && target.tagName === 'CODE') {
                // Add click event listener to the code element.

                target.addEventListener('click', () => {
                    // Copy the text content.

                    set_paste_buffer_to_text(target.textContent);

                    // Update the icon classes.

                    element.classList.add('fas');
                    element.classList.remove('far');

                    // Reset the icon after 250ms.

                    setTimeout(() => {
                        element.classList.add('far');
                        element.classList.remove('fas');
                    }, 250);
                });
            }
        });

        // Handle external links in page content.

        const links = document.querySelectorAll('section.page-content a');

        links.forEach(anchor => {
            if (!(location.hostname === anchor.hostname || !anchor.hostname.length)) {
                anchor.setAttribute('target', '_blank');
            }
        });

        // Handle image preview in page content.

        const images = document.querySelectorAll('section.page-content img');

        images.forEach(image => {
            image.addEventListener('click', () => {
                dashboard.preview_image(image.src, image.alt);
            });
        });
    });

    // Table of clickable actions and their handlers. Clickable actions will
    // be registered on page load and each have a unique incrementing integer
    // ID appended to "clickable-action-" as their element ID.

    const clickable_action_handlers = {};
    const clickable_actions = {};

    function register_clickable_action(action, args) {
        const element = document.getElementById(action);
        const handler = element.dataset.handler;
        const callback = clickable_action_handlers[handler];

        console.log("register_clickable_action", handler, action);

        clickable_actions[action] = function () {
            callback(element, args);
        };
    }

    function trigger_clickable_action(event) {
        const element = event.currentTarget;
        const action = element.id;
        const handler = element.dataset.handler;

        console.log("clickable_action_handler", handler, action);

        const callback = clickable_actions[action];

        if (callback) {
            callback();
        }
    }

    function clickable_action_handler(name, handler) {
        clickable_action_handlers[name] = handler;
    }

    // Register built-in clickable action handlers.

    clickable_action_handler("terminal:execute", function (element, args) {
        const defaults = {
            "command": undefined,
            "session": "1",
            "clear": false,
        }

        args = { ...defaults, ...args }

        const command = args.command;
        const session = args.session || "1";
        const clear = args.clear;

        if (!command) {
            return;
        }

        terminals.execute_in_terminal(command, session, clear);
    });

    clickable_action_handler("terminal:execute-all", function (element, args) {
        const defaults = {
            "command": undefined,
            "clear": false,
        }

        args = { ...defaults, ...args }

        const command = args.command;
        const clear = args.clear;

        if (!command) {
            return;
        }

        terminals.execute_in_all_terminals(command, clear);
    });

    clickable_action_handler("terminal:interrupt", function (element, args) {
        const defaults = {
            "session": "1",
        }

        args = { ...defaults, ...args }

        const session = args.session;

        terminals.interrupt_terminal(session);
    });

    clickable_action_handler("terminal:interrupt-all", function (element, args) {
        terminals.interrupt_all_terminals();
    });

    clickable_action_handler("terminal:clear", function (element, args) {
        const defaults = {
            "session": "1",
        }

        args = { ...defaults, ...args }

        const session = args.session;

        terminals.clear_terminal(session);
    });

    clickable_action_handler("terminal:clear-all", function (element, args) {
        terminals.clear_all_terminals();
    });

    // Exported functions.

    function paste_to_terminal(text, session) {
        session = session || "1";

        if (session == "*") {
            if (!dashboard.expose_dashboard("terminal")) {
                return false;
            }
            terminals.paste_to_all_terminals(text);
        } else {
            if (!dashboard.expose_terminal(session)) {
                return false;
            }
            terminals.paste_to_terminal(text, session);
            return true;
        }
    }

    function paste_to_all_terminals(text) {
        if (!dashboard.expose_dashboard("terminal")) {
            return false;
        }
        terminals.paste_to_all_terminals(text);
        return true;
    }

    function execute_in_terminal(command, session, clear = false) {
        session = session || "1";

        if (session == "*") {
            if (!dashboard.expose_dashboard("terminal")) {
                return false;
            }
            terminals.execute_in_all_terminals(command, clear);
        } else {
            if (!dashboard.expose_terminal(session)) {
                return false;
            }
            terminals.execute_in_terminal(command, session, clear);
        }
        return true;
    }

    function execute_in_all_terminals(command, clear = false) {
        if (!dashboard.expose_dashboard("terminal")) {
            return false;
        }
        terminals.execute_in_all_terminals(command, clear);
        return true;
    }

    function clear_terminal(session) {
        session = session || "1";

        if (session == "*") {
            if (!dashboard.expose_dashboard("terminal")) {
                return false;
            }
            terminals.clear_all_terminals();
        } else {
            if (!dashboard.expose_terminal(session)) {
                return false;
            }
            terminals.clear_terminal(session);
        }
        return true;
    }

    function clear_all_terminals() {
        if (!dashboard.expose_dashboard("terminal")) {
            return false;
        }
        terminals.clear_all_terminals();
        return true;
    }

    function interrupt_terminal(session) {
        session = session || "1";

        if (session == "*") {
            if (!dashboard.expose_dashboard("terminal")) {
                return false;
            }
            terminals.interrupt_all_terminals();
        } else {
            if (!dashboard.expose_terminal(session)) {
                return false;
            }
            terminals.interrupt_terminal(session);
        }
        return true;
    }

    function interrupt_all_terminals() {
        terminals.interrupt_all_terminals();
    }

    function expose_terminal(session) {
        if (!dashboard.expose_terminal(session)) {
            return false;
        }
        terminals.select_terminal(session);
        return true;
    }

    function expose_dashboard(name) {
        return dashboard.expose_dashboard(name);
    }

    function create_dashboard(name, url, focus) {
        return dashboard.create_dashboard(name, url, focus);
    }

    function delete_dashboard(name) {
        return dashboard.delete_dashboard(name);
    }

    function reload_dashboard(name, url, focus) {
        return dashboard.reload_dashboard(name, url, focus);
    }

    function collapse_workshop() {
        dashboard.collapse_workshop();
    }

    function reload_workshop() {
        dashboard.reload_workshop();
    }

    function finished_workshop() {
        dashboard.finished_workshop();
    }

    function terminate_session() {
        dashboard.terminate_session();
    }

    function preview_image(src, title) {
        dashboard.preview_image(src, title);
    }

    return {
        register_clickable_action,
        trigger_clickable_action,
        paste_to_terminal,
        paste_to_all_terminals,
        execute_in_terminal,
        execute_in_all_terminals,
        clear_terminal,
        clear_all_terminals,
        interrupt_terminal,
        interrupt_all_terminals,
        expose_terminal,
        expose_dashboard,
        create_dashboard,
        delete_dashboard,
        reload_dashboard,
        collapse_workshop,
        reload_workshop,
        finished_workshop,
        terminate_session,
        preview_image,
    };
})();

window.educates = educates;
