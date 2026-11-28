export const extensionStyle = `
    #gowarp-toolkit-dialog {box-sizing:border-box;width:min(640px,92vw);max-height:88vh;overflow:auto;padding:24px;border:1px solid #666;border-radius:10px;background:#202020;color:#eee;color-scheme:dark;font:14px/1.5 sans-serif;box-shadow:0 12px 50px #0008;}
    #gowarp-toolkit-dialog::backdrop {background:#0008;}
    #gowarp-toolkit-dialog h2 {margin:0;color:#ff4b51;font-size:22px;}
    #gowarp-toolkit-dialog p {margin:10px 0;}
    #gowarp-toolkit-dialog .em-header,#gowarp-toolkit-dialog .em-footer,#gowarp-toolkit-dialog .em-toolbar,#gowarp-toolkit-dialog .em-summary {display:flex;align-items:center;gap:8px;flex-wrap:wrap;}
    #gowarp-toolkit-dialog .em-header {justify-content:space-between;}
    #gowarp-toolkit-dialog .em-muted {color:#aaa;font-size:12px;}
    #gowarp-toolkit-dialog .em-warning {border-left:3px solid #ff8187;padding:8px 12px;background:#2c2223;color:#e8c2c3;font-size:12px;}
    #gowarp-toolkit-dialog button {padding:8px 12px;border:1px solid #666;border-radius:5px;background:#333;color:#fff;font:inherit;cursor:pointer;}
    #gowarp-toolkit-dialog button:hover:not(:disabled) {background:#444;}
    #gowarp-toolkit-dialog button:disabled {opacity:.45;cursor:default;}
    #gowarp-toolkit-dialog button[data-primary] {background:#a32a31;border-color:#c85e63;}
    #gowarp-toolkit-dialog button[data-primary]:hover:not(:disabled) {background:#c52f3b;}
    #gowarp-toolkit-dialog button:focus-visible,#gowarp-toolkit-dialog input:focus-visible,#gowarp-toolkit-dialog select:focus-visible {outline:2px solid #ff8187;outline-offset:2px;}
    #gowarp-toolkit-dialog input[type=checkbox] {accent-color:#c52f3b;width:16px;height:16px;margin:0;flex:none;}
    #gowarp-toolkit-dialog select {box-sizing:border-box;max-width:100%;min-width:0;padding:7px 9px;border:1px solid #666;border-radius:5px;background:#111;color:#eee;font:13px sans-serif;}
    #gowarp-toolkit-dialog .em-summary {margin:18px 0 8px;justify-content:space-between;}
    #gowarp-toolkit-dialog .em-toolbar {padding:10px 0;}
    #gowarp-toolkit-dialog .em-toolbar button {font-size:12px;padding:6px 9px;}
    #gowarp-toolkit-dialog .em-list {display:grid;gap:6px;max-height:34vh;overflow:auto;overscroll-behavior:contain;}
    #gowarp-toolkit-dialog .em-item {border:1px solid #484848;border-radius:6px;padding:10px 12px;background:#292929;display:grid;grid-template-columns:minmax(0,1fr) auto;gap:6px 12px;align-items:center;}
    #gowarp-toolkit-dialog .em-item label {display:flex;align-items:center;gap:8px;min-width:0;font-weight:600;overflow-wrap:anywhere;cursor:pointer;}
    #gowarp-toolkit-dialog .em-detail {grid-column:1/-1;color:#aaa;font:12px/1.5 monospace;overflow-wrap:anywhere;}
    #gowarp-toolkit-dialog .em-empty {padding:20px 10px;border:1px dashed #555;border-radius:6px;text-align:center;color:#aaa;}
    #gowarp-toolkit-dialog .em-status {font-size:12px;color:#ccc;overflow-wrap:anywhere;min-height:18px;}
    #gowarp-toolkit-dialog .em-footer {border-top:1px solid #484848;padding-top:14px;margin-top:14px;}
    #gowarp-toolkit-dialog .em-footer .em-spacer {flex:1;}
    @media(max-width:500px) {#gowarp-toolkit-dialog {padding:16px;} #gowarp-toolkit-dialog .em-item {grid-template-columns:1fr;} #gowarp-toolkit-dialog .em-detail {grid-column:1;}}
`;