import type {Shape} from './type';

export function round (v) {
    return Math.round(v * 1000) / 1000;
}
function normalizePoints (points) {
    var xs = points.map(function (p) { return p[0]; });
    var ys = points.map(function (p) { return p[1]; });
    var minX = Math.min.apply(null, xs);
    var minY = Math.min.apply(null, ys);
    var w = Math.max.apply(null, xs) - minX || 1;
    var h = Math.max.apply(null, ys) - minY || 1;
    return points.map(function (p) {
        return [round((p[0] - minX) / w * 100), round((p[1] - minY) / h * 100)];
    });
}
function polygonPath (points) {
    return points.map(function (p, i) {
        return (i === 0 ? 'M' : 'L') + p[0] + ',' + p[1];
    }).join(' ') + 'Z';
}
export function cornerPath (points, radius) {
    if (!radius || radius <= 0) return polygonPath(points);
    var count = points.length;
    var path = '';
    for (var i = 0; i < count; i++) {
        var previous = points[(i + count - 1) % count];
        var point = points[i];
        var next = points[(i + 1) % count];
        var entryLength = Math.hypot(point[0] - previous[0], point[1] - previous[1]);
        var exitLength = Math.hypot(next[0] - point[0], next[1] - point[1]);
        var cut = Math.min(radius, entryLength / 2, exitLength / 2);
        var entry = [point[0] + ((previous[0] - point[0]) / (entryLength || 1)) * cut,
            point[1] + ((previous[1] - point[1]) / (entryLength || 1)) * cut];
        var exit = [point[0] + ((next[0] - point[0]) / (exitLength || 1)) * cut,
            point[1] + ((next[1] - point[1]) / (exitLength || 1)) * cut];
        path += (i === 0 ? 'M' : 'L') + round(entry[0]) + ',' + round(entry[1]) + ' ';
        path += 'Q' + point[0] + ',' + point[1] + ' ' + round(exit[0]) + ',' + round(exit[1]) + ' ';
    }
    return path + 'Z';
}
function regularPolygon (sides, startDegrees) {
    var points = [];
    for (var i = 0; i < sides; i++) {
        var angle = ((startDegrees + (i * 360 / sides)) * Math.PI) / 180;
        points.push([50 + (50 * Math.cos(angle)), 50 + (50 * Math.sin(angle))]);
    }
    return normalizePoints(points);
}
function rotatePoints (points, degrees) {
    var radians = degrees * Math.PI / 180;
    var cos = Math.cos(radians);
    var sin = Math.sin(radians);
    return normalizePoints(points.map(function (p) {
        var x = p[0] - 50;
        var y = p[1] - 50;
        return [50 + (x * cos) - (y * sin), 50 + (x * sin) + (y * cos)];
    }));
}
function starPoints (tips, inner) {
    var points = [];
    for (var i = 0; i < tips * 2; i++) {
        var radius = i % 2 === 0 ? 50 : inner;
        var angle = ((-90 + (i * 180 / tips)) * Math.PI) / 180;
        points.push([50 + (radius * Math.cos(angle)), 50 + (radius * Math.sin(angle))]);
    }
    return normalizePoints(points);
}
function crescentPoints () {
    var points = [];
    var i;
    for (i = 0; i <= 24; i++) {
        var a = (-90 + (i * 180 / 24)) * Math.PI / 180;
        points.push([50 + (50 * Math.cos(a)), 50 + (50 * Math.sin(a))]);
    }
    for (i = 24; i >= 0; i--) {
        var b = (-90 + (i * 180 / 24)) * Math.PI / 180;
        points.push([50 + (50 * Math.cos(b)) + 26 * Math.sin(b), 50 + (50 * Math.sin(b))]);
    }
    return normalizePoints(points);
}

var PLUS = [
    [35, 0], [65, 0], [65, 35], [100, 35], [100, 65], [65, 65],
    [65, 100], [35, 100], [35, 65], [0, 65], [0, 35], [35, 35]
];

export var SHAPES: Shape[] = [
    {id: 'triangle', label: 'Üçgen', en: 'Triangle', points: [[50, 0], [100, 100], [0, 100]]},
    {id: 'right-triangle', label: 'Dik üçgen', en: 'Right triangle', points: [[0, 0], [100, 100], [0, 100]]},
    {id: 'rhombus', label: 'Eşkenar dörtgen', en: 'Rhombus', points: [[50, 0], [100, 50], [50, 100], [0, 50]]},
    {id: 'parallelogram', label: 'Paralelkenar', en: 'Parallelogram', points: [[22, 0], [100, 0], [78, 100], [0, 100]]},
    {id: 'trapezoid', label: 'Yamuk', en: 'Trapezoid', points: [[24, 0], [76, 0], [100, 100], [0, 100]]},
    {id: 'pentagon', label: 'Beşgen', en: 'Pentagon', points: regularPolygon(5, -90)},
    {id: 'hexagon', label: 'Altıgen', en: 'Hexagon', points: regularPolygon(6, -90)},
    {id: 'heptagon', label: 'Yedigen', en: 'Heptagon', points: regularPolygon(7, -90)},
    {id: 'octagon', label: 'Sekizgen', en: 'Octagon', points: regularPolygon(8, -67.5)},
    {id: 'star5', label: 'Yıldız', en: 'Star', points: starPoints(5, 20)},
    {id: 'star6', label: 'Altı köşeli yıldız', en: 'Six-pointed star', points: starPoints(6, 28)},
    {id: 'soft-triangle', label: 'Yumuşak üçgen', en: 'Rounded triangle', points: [[50, 0], [100, 100], [0, 100]], minRound: 14},
    {id: 'soft-pentagon', label: 'Yumuşak beşgen', en: 'Rounded pentagon', points: regularPolygon(5, -90), minRound: 12},
    {id: 'soft-hexagon', label: 'Yumuşak altıgen', en: 'Rounded hexagon', points: regularPolygon(6, -90), minRound: 12},
    {id: 'soft-square', label: 'Yumuşak kare', en: 'Rounded square', points: [[0, 0], [100, 0], [100, 100], [0, 100]], minRound: 22},
    {id: 'half-circle', label: 'Yarım daire', en: 'Half circle',
        path: 'M0,100 L0,50 A50,50 0 0 1 100,50 L100,100 Z'},
    {id: 'quarter-circle', label: 'Çeyrek daire', en: 'Quarter circle',
        path: 'M0,100 L0,0 A100,100 0 0 1 100,100 Z'},
    {id: 'ring', label: 'Halka', en: 'Ring', fillRule: 'evenodd',
        path: 'M50,0 A50,50 0 1 0 50,100 A50,50 0 1 0 50,0 Z M50,30 A20,20 0 1 1 50,70 A20,20 0 1 1 50,30 Z'},
    {id: 'drop', label: 'Damla', en: 'Drop',
        path: 'M50,2 C62,20 86,38 86,62 C86,82 70,98 50,98 C30,98 14,82 14,62 C14,38 38,20 50,2 Z'},
    {id: 'crescent', label: 'Hilal', en: 'Crescent', points: crescentPoints()},
    {id: 'arrow-up', label: 'Yukarı ok', en: 'Arrow up',
        points: [[50, 0], [100, 50], [65, 50], [65, 100], [35, 100], [35, 50], [0, 50]]},
    {id: 'arrow-down', label: 'Aşağı ok', en: 'Arrow down',
        points: [[50, 100], [100, 50], [65, 50], [65, 0], [35, 0], [35, 50], [0, 50]]},
    {id: 'arrow-right', label: 'Sağ ok', en: 'Arrow right',
        points: [[0, 35], [50, 35], [50, 0], [100, 50], [50, 100], [50, 65], [0, 65]]},
    {id: 'arrow-left', label: 'Sol ok', en: 'Arrow left',
        points: [[100, 35], [50, 35], [50, 0], [0, 50], [50, 100], [50, 65], [100, 65]]},
    {id: 'arrow-double', label: 'Çift ok', en: 'Double arrow',
        points: [[0, 50], [25, 20], [25, 35], [75, 35], [75, 20], [100, 50], [75, 80], [75, 65], [25, 65], [25, 80]]},
    {id: 'heart', label: 'Kalp', en: 'Heart',
        path: 'M50,90 C20,66 2,48 2,29 C2,13 14,2 28,2 C38,2 46,8 50,17 ' +
            'C54,8 62,2 72,2 C86,2 98,13 98,29 C98,48 80,66 50,90 Z'},
    {id: 'cloud', label: 'Bulut', en: 'Cloud',
        path: 'M26,80 C11,80 2,69 2,55 C2,42 11,32 23,30 C27,15 41,4 57,4 ' +
            'C74,4 88,16 91,32 C97,36 100,43 100,51 C100,67 89,80 73,80 Z'},
    {id: 'bolt', label: 'Şimşek', en: 'Bolt',
        points: [[58, 0], [16, 56], [43, 56], [34, 100], [86, 40], [57, 40], [78, 0]]},
    {id: 'bubble', label: 'Konuşma balonu', en: 'Speech bubble', minRound: 6,
        points: [[12, 4], [88, 4], [96, 12], [96, 62], [88, 70], [42, 70], [26, 88], [28, 70], [12, 70], [4, 62], [4, 12]]},
    {id: 'plus', label: 'Artı', en: 'Plus', points: PLUS},
    {id: 'cross', label: 'Çarpı', en: 'Cross', points: rotatePoints(PLUS, 45)}
];
SHAPES.forEach(function (shape) {
    shape.d = shape.points ? cornerPath(shape.points, shape.minRound || 0) : shape.path;
});

export var NIBS = [
    {label: 'Kıl uç', en: 'Fine tip', size: 1}, {label: 'İnce uç', en: 'Thin tip', size: 2},
    {label: 'Ucu ince kalem', en: 'Pencil tip', size: 3}, {label: 'Tükenmez', en: 'Ballpoint', size: 5},
    {label: 'Keçe ucu', en: 'Felt tip', size: 8}, {label: 'Boya kalemi', en: 'Crayon', size: 12},
    {label: 'Kalın kalem', en: 'Thick pen', size: 18}, {label: 'Fırça', en: 'Brush', size: 26},
    {label: 'Kalın fırça', en: 'Thick brush', size: 40}, {label: 'Boya rulosu', en: 'Roller', size: 60},
    {label: 'Devasa', en: 'Huge', size: 100}
];

export var PENS = [
    {id: 'kalem', label: 'Kalem', en: 'Pen', min: 0.72, exponent: 0.35, taper: 0.3, smoothing: 0.3},
    {id: 'tukenmez', label: 'Tükenmez', en: 'Ballpoint', min: 0.92, exponent: 0.15, taper: 0.1, smoothing: 0.25},
    {id: 'firca', label: 'Fırça', en: 'Brush', min: 0.15, exponent: 1.4, taper: 0.45, smoothing: 0.5},
    {id: 'marker', label: 'Kalın marker', en: 'Marker', min: 0.95, exponent: 0.1, taper: 0.05, smoothing: 0.2},
    {id: 'kaligrafi', label: 'Kaligrafi', en: 'Calligraphy', min: 0.3, exponent: 0.8, taper: 0.25, smoothing: 0.35, chisel: true},
    {id: 'murekkep', label: 'Mürekkep', en: 'Ink', min: 0.05, exponent: 2.1, taper: 0.7, smoothing: 0.45}
];
