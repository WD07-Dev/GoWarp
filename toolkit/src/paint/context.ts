import type {EditorDocument, EditorWindow} from './type';
import type {createEditorAccess} from './editor';
import type {createLifecycle} from '../shared/lifecycle';
import type {createTheme} from './theme';

export interface PaintContext {
    document: EditorDocument;
    window: EditorWindow;
    editor: ReturnType<typeof createEditorAccess>;
    lifecycle: ReturnType<typeof createLifecycle>;
    theme: ReturnType<typeof createTheme>;
}
