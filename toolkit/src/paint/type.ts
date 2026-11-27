export interface PaintTools {
    openPixelEditor(): void;
    dispose(): void;
}

export type EditorWindow = Window & typeof globalThis & {
    paper?: any;
    ReduxStore?: any;
    webpackChunkGUI?: any;
    webpackJsonpGUI?: any;
};

export interface Shape {
    id: string;
    label: string;
    en: string;
    points?: number[][];
    minRound?: number;
    path?: string;
    fillRule?: string;
    d?: string;
}

export interface AlphaStats {
    area: number;
    w: number;
    h: number;
    thumb: string;
    cx?: number;
    cy?: number;
    box?: number[];
}

export interface PixelEditor {
    root: PaintElement;
    src: any;
    target: any;
    costume: string;
    w: number;
    h: number;
    offX: number;
    offY: number;
    unit: number;
    x0: number;
    y0: number;
    grid: ImageData;
    undo: ImageData[];
    redo: ImageData[];
    zoom: number;
    tool: string;
    color: string;
    rgba: number[];
    size: number;
    filled: boolean;
    gridOn: boolean;
    dark: boolean;
    gesture: any;
    busy: boolean;
    tmp: PaintElement;
    canvas?: PaintElement;
    stage?: PaintElement;
    note?: PaintElement;
    colorInput?: PaintElement;
    sizeInput?: PaintElement;
    sizeOut?: PaintElement;
    autoFixInput?: PaintElement;
    zoomOut?: PaintElement;
    btnUndo?: PaintElement;
    btnRedo?: PaintElement;
    setTool?: (tool: string) => void;
    onKey?: (event: KeyboardEvent) => void;
    onResize?: () => void;
}

export type PaintElement = HTMLElement & {[key: string]: any};
export type EditorDocument = Omit<Document, 'createElement' | 'querySelector' | 'getElementById'> & {
    createElement(tag: string): PaintElement;
    querySelector(selector: string): PaintElement;
    getElementById(id: string): PaintElement;
    __gwtGrad?: boolean;
};

export interface AnchorServices {
    anchorTarget: () => any;
    findVM: () => any;
    readonly railEl: any;
    markObjects: () => void;
    keyRow: (chips: any) => PaintElement;
    readonly openPanel: any;
    toggleFor: (anchor: any, kind: any, extraClass?: any) => PaintElement;
    labelEl: (text: any) => PaintElement;
}