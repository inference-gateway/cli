import {Extension} from 'resource:///org/gnome/shell/extensions/extension.js';

export default class Computer extends Extension {
    enable() {
        global.context.unsafe_mode = true;
    }

    disable() {
        global.context.unsafe_mode = false;
    }
}
