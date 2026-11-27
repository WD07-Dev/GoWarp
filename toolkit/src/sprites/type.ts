export interface FolderMenuItem {
    head?: string;
    label?: string;
    icon?: string;
    checked?: boolean;
    sep?: boolean;
    run?: () => void;
}

export interface FolderServices {
    findVM: () => any;
    ensureStyle: () => void;
}

export interface SpriteInfo {
    id: string;
    name: string;
}

export interface FolderData {
    folders: {id: string; name: string}[];
    marks: {n: string; f: string}[];
}
