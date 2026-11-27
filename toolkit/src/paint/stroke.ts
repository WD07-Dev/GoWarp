function segmentCount(path: any): number {
    return path.children ? path.children.reduce((count: number, child: any) => count + segmentCount(child), 0) : path.segments.length;
}

function hasOpaqueFill(path: any, color: any): boolean {
    return (path.className === 'Path' || path.className === 'CompoundPath') &&
    path.visible && !path.locked && !path.guide && !path.clipMask &&
    path.opacity === 1 && path.blendMode === 'normal' && !path.strokeColor &&
    path.fillColor?.type !== 'gradient' && path.fillColor?.alpha === 1 &&
    path.fillColor.equals(color) && (!path.children || path.children.every((child: any) => hasOpaqueFill(child, color)));
}

export function preparePenStroke(path: any): {path: any; previous: any} {
    const clean = path.unite(path, {insert: false});
    clean.data.gwtPenStroke = true;
    const previous = path.previousSibling;
    if(!previous?.data.gwtPenStroke || !hasOpaqueFill(clean, clean.fillColor) || !hasOpaqueFill(previous, clean.fillColor) || !clean.bounds.intersects(previous.bounds)) {
        return {path: clean, previous: null};
    }

    const merged = clean.unite(previous, {insert: false});
    if(segmentCount(merged) >= segmentCount(clean) + segmentCount(previous)) {
        merged.remove();
        return {path: clean, previous: null};
    }
    clean.remove();
    return {path: merged, previous};
}