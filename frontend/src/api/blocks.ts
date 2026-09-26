// Блоки документа в ответе читателю: дискриминируемое объединение по полю type.
// Формы данных генерирует tygo из Go (documents.Out*); здесь они только собраны в объединение.
import type {
  OutAppendix,
  OutBlock,
  OutClipping,
  OutContainmentProcedure,
  OutDirective,
  OutDivider,
  OutDocLink,
  OutExperimentLog,
  OutFootnote,
  OutHeading,
  OutAudio,
  OutHypothesis,
  OutImage,
  OutIncidentTimeline,
  OutList,
  OutMemo,
  OutPage,
  OutParagraph,
  OutPersonnelRecord,
  OutQA,
  OutQuote,
  OutRoster,
  OutRouting,
  OutStamp,
  OutTable,
} from './generated/documents'

/** Закрытый блок: ни текста, ни данных, только нужный уровень допуска. */
export interface RedactedData {
  level: number
}

type Tagged<K extends string, D> = { id?: string; type: K; data: D }

export type DocBlock =
  | Tagged<'heading', OutHeading>
  | Tagged<'paragraph', OutParagraph>
  | Tagged<'list', OutList>
  | Tagged<'quote', OutQuote>
  | Tagged<'dossier_header', Record<string, never>>
  | Tagged<'experiment_log', OutExperimentLog>
  | Tagged<'stamp', OutStamp>
  | Tagged<'memo', OutMemo>
  | Tagged<'clipping', OutClipping>
  | Tagged<'table', OutTable>
  | Tagged<'doc_link', OutDocLink>
  | Tagged<'divider', OutDivider>
  | Tagged<'page', OutPage>
  | Tagged<'footnote', OutFootnote>
  | Tagged<'appendix', OutAppendix>
  | Tagged<'containment_procedure', OutContainmentProcedure>
  | Tagged<'directive', OutDirective>
  | Tagged<'incident_timeline', OutIncidentTimeline>
  | Tagged<'personnel_record', OutPersonnelRecord>
  | Tagged<'roster', OutRoster>
  | Tagged<'hypothesis', OutHypothesis>
  | Tagged<'qa', OutQA>
  | Tagged<'routing', OutRouting>
  | Tagged<'image', OutImage>
  | Tagged<'audio', OutAudio>
  | Tagged<'redacted', RedactedData>

export type BlockKind = DocBlock['type']

/**
 * Сервер шлёт OutBlock с data: unknown. Форму каждого вида гарантирует сервер (он собирает ответ поле за полем),
 * поэтому здесь — одно осознанное сужение типа; неизвестный вид блока (из более новой версии сервера) рисуется как
 * закрытый и не ломает страницу.
 */
export function asDocBlocks(blocks: OutBlock[]): DocBlock[] {
  return blocks as DocBlock[]
}
