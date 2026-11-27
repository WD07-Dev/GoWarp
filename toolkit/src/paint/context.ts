import type {EditorDocument, EditorWindow} from './type';
import type {createEditorAccess} from './editor';
import type {createLifecycle} from '../shared/lifecycle';
import type {createLocale} from './locale';
import type {createTheme} from './theme';

export interface PaintContext {
    document: EditorDocument;
    window: EditorWindow;
    editor: ReturnType<typeof createEditorAccess>;
    lifecycle: ReturnType<typeof createLifecycle>;
    locale: ReturnType<typeof createLocale>;
    theme: ReturnType<typeof createTheme>;
}
