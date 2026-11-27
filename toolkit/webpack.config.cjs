const path = require('node:path');
const fs = require('node:fs');
const webpack = require('webpack');

module.exports = {
    context: __dirname,
    entry: './src/index.ts',
    target: 'web',
    output: {
        path: path.resolve(__dirname, '../app/assets/embed'),
        filename: 'gowarp-toolkit.js'
    },
    resolve: {
        extensions: ['.ts', '.js']
    },
    module: {
        rules: [
            {
                test: /\.ts$/,
                use: 'ts-loader',
                exclude: /node_modules/
            }
        ]
    },
    optimization: {
        minimize: false
    },
    devtool: false,
    plugins: [
        new webpack.BannerPlugin({
            raw: true,
            banner: fs.readFileSync(path.join(__dirname, 'banner.txt'), 'utf8')
        })
    ]
};