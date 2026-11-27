export interface ScratchHost {
    vm: any;
    translate: {
        language: string
    };
    extensions: {
        unsandboxed: boolean;
        register(extension: object): void;
    };
    BlockType: {
        BUTTON: string;
        REPORTER: string
    };
}