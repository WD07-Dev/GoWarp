export interface Project {
    extensions?: string[];
    extensionURLs?: Record<string, string>;
}

export interface Review {
    project: {extensionURLs: Record<string, string>};
    resolve: () => void;
    reject: (error: Error) => void;
}