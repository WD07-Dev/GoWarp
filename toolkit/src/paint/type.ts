import { createLifecycle } from "../shared/lifecycle";
import { createEditorAccess } from "./editor";
import { createTheme } from "./theme";

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
    nameKey: string;
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
    src: PixelSource;
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
    keyRow: (chips: string[]) => PaintElement;
    readonly openPanel: any;
    toggleFor: (anchor: PaintElement, kind: string, extraClass?: string) => PaintElement;
    labelEl: (text: string) => PaintElement;
}

export interface PaintContext {
    document: EditorDocument;
    window: EditorWindow;
    editor: ReturnType<typeof createEditorAccess>;
    lifecycle: ReturnType<typeof createLifecycle>;
    theme: ReturnType<typeof createTheme>;
}

export interface LayerServices {
    paintingLayer: () => any;
    selectedPaths: () => any[];
    commit: () => boolean;
    eachSelected: (fn: (item: any) => void, live?: boolean) => boolean;
    icon: (paths: string) => string;
    paintEditorOpen: () => boolean;
    colorSig: (color: any) => any;
}

export interface RGBA {
    r: number;
    g: number;
    b: number;
    a: number;
}

export interface PaintColor {
    primary?: string;
    secondary?: string;
    gradientType?: string;
}

export interface PaintSettings {
    nib: number;
    eraser: number;
    bitNib: number;
    bitEraser: number;
    shapeSize: number;
    shapeRound: number;
    pen: string;
    penSize: number;
    penPressure: boolean;
    pngScale: number;
    grad: {
        count: number;
        offsets: number[];
        extras: string[];
        stops?: {p: number; c: string}[];
    };
}

export interface AnchorMeasurement extends AlphaStats {
    index: number;
    name: string;
    pivot: number[];
}

export interface PixelSource {
    canvas: PaintElement;
    ctx: CanvasRenderingContext2D;
    W: number;
    H: number;
    s: number;
    target: any;
    costume: any;
    costumeW: number;
    costumeH: number;
}

export interface PixelBox {
    x: number;
    y: number;
    w: number;
    h: number;
}

export interface PixelGroup extends RGBA {
    rects: PixelBox[];
}

export interface PixelVectorPath {
    boxes: PixelBox[];
    color: PixelGroup;
    runs: number;
}

export interface PixelServices {
    anchorTarget: () => any;
    rasterItem: () => any;
    commit: () => boolean;
    paintEditorOpen: () => boolean;
    icon: (paths: string) => string;
    clamp: (v: number, a: number, b: number) => number;
    paintingLayer: () => any;
    findLayer: (flag: string) => any;
    refreshAnchorView: (t: any) => void;
    GLYPH: { size: string; round: string; nib: string; undo: string; wheel: string; plus: string; trash: string; };
}