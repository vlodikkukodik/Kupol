import type { InputBlock } from '@/api/generated/documents'

export const block = (type: string, data: unknown, extra: Partial<InputBlock> = {}): InputBlock => ({ id: `${type}1`, type, data, ...extra })

/** Каноническая форма — та, что хранит сервер: её преобразование туда и обратно не меняет ни знака. Все 15 видов блоков. */
export const canonical: InputBlock[] = [
  block('dossier_header', {}),
  block('heading', { depth: 2, text: 'Общие сведения' }),
  block(
    'paragraph',
    {
      text: [{ text: 'Обычный ' }, { text: 'жирный', bold: true }, { text: ' и ' }, { text: 'курсив', italic: true }, { text: ' ' }, { text: 'СЕКРЕТ', level: 3 }],
    },
    { level: 2 },
  ),
  block('list', { ordered: true, items: [[{ text: 'Один' }], [{ text: 'Два ' }, { text: 'три', level: 4 }]] }),
  block('list', { items: [[{ text: 'Маркер' }]] }, { id: 'list2' }),
  block('quote', { text: [{ text: 'Оно не приближается.' }], source: 'свидетель' }),
  block('quote', { text: [{ text: 'Без источника.' }] }, { id: 'quote2' }),
  block('experiment_log', {
    title: 'Журнал',
    entries: [
      { date: '12.04.1979', participants: ['Дежурный', 'Лаборант'], text: [{ text: 'Помещён в камеру.' }] },
      { text: [{ text: 'Запись без даты.' }] },
    ],
  }),
  block('stamp', { text: 'Совершенно секретно', tone: 'red', tilt: -6 }),
  block('stamp', { text: 'Копия', tone: 'ink' }, { id: 'stamp2' }),
  block('memo', {
    kind: 'order',
    number: 'МЕМО-5',
    date: '20.03.1979',
    from: 'Отдел 2',
    to: ['Директорат', 'Архив'],
    subject: 'О порядке',
    body: [[{ text: 'Первый абзац.' }], [{ text: 'Второй абзац.' }]],
    signature: 'Начальник',
  }),
  block('memo', { kind: 'memo', body: [[{ text: 'Коротко.' }]] }, { id: 'memo2' }),
  block('clipping', {
    kind: 'newspaper',
    title: 'Огни',
    source: 'Вечерний',
    date: '02.03.1979',
    paragraphs: [[{ text: 'Свечение.' }], [{ text: 'Без комментариев.' }]],
  }),
  block(
    'clipping',
    {
      kind: 'transcript',
      title: 'Запись № 3',
      lines: [{ speaker: 'Следователь', text: [{ text: 'Что вы видели?' }] }, { text: [{ text: 'Реплика без говорящего.' }] }],
    },
    { id: 'clip2' },
  ),
  block('table', {
    caption: 'Замеры',
    columns: ['Дата', 'Показатель'],
    rows: [[[{ text: '01.04' }], [{ text: '12' }]], [[{ text: '02.04' }], [{ text: 'закрыто', level: 3 }]], [[{ text: '' }], [{ text: '' }]]],
  }),
  block('doc_link', { code: 'О-9002', note: 'Смежный' }),
  block('doc_link', { code: 'ПРИКАЗ-1901-91' }, { id: 'link2' }),
  block('divider', { style: 'stars' }),
  block('divider', { style: 'line' }, { id: 'div2' }),
  block('page', { number: '2' }),
  block('page', {}, { id: 'page2' }),
  block('footnote', { mark: '*', text: [{ text: 'Сноска.' }] }),
  block('appendix', { number: '1', title: 'Схема' }),
  block('appendix', { title: 'Без номера' }, { id: 'app2' }),
]
