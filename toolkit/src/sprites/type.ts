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
    pageToast: (text: any, bad?: any) => void;
    ensureStyle: () => void;
}