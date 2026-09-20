// Поля блоков, которые правятся обычными полями ввода в шапке блока (а не набираются в тексте).
// Один перечень на всё: по нему строится представление узла, по нему же проверяются значения по умолчанию.
// Названия видов блоков — те же, что отдаёт сервер (documents.blockKindNames): редактору они нужны сразу, без запроса.

export interface Option {
  value: string
  label: string
}

interface Base {
  /** Имя атрибута узла */
  key: string
  label: string
  /** Поле на всю ширину */
  wide?: boolean
}

export type FieldSpec =
  | (Base & { kind: 'text'; maxLength?: number })
  | (Base & { kind: 'select'; options: Option[] })
  | (Base & { kind: 'number'; min: number; max: number })
  /** Список строк: по одному значению на строке (запятая внутри имени допустима) */
  | (Base & { kind: 'lines'; hint?: string })

/** Узел редактора → перечень полей его шапки. Узлов без полей здесь нет. */
export const FIELDS: Record<string, FieldSpec[]> = {
  heading: [
    {
      key: 'depth',
      label: 'Размер',
      kind: 'select',
      options: [
        { value: '1', label: 'Крупный' },
        { value: '2', label: 'Средний' },
        { value: '3', label: 'Мелкий' },
      ],
    },
  ],
  list: [
    {
      key: 'ordered',
      label: 'Вид списка',
      kind: 'select',
      options: [
        { value: 'false', label: 'Маркеры' },
        { value: 'true', label: 'Нумерация' },
      ],
    },
  ],
  quote: [{ key: 'source', label: 'Источник цитаты', kind: 'text', maxLength: 200, wide: true }],
  experimentLog: [{ key: 'title', label: 'Название журнала', kind: 'text', maxLength: 200, wide: true }],
  logEntry: [
    { key: 'date', label: 'Дата записи', kind: 'text', maxLength: 40 },
    { key: 'participants', label: 'Участники', kind: 'lines', hint: 'по одному на строке' },
  ],
  stamp: [
    { key: 'text', label: 'Текст штампа', kind: 'text', maxLength: 40, wide: true },
    {
      key: 'tone',
      label: 'Цвет',
      kind: 'select',
      options: [
        { value: 'red', label: 'Красный' },
        { value: 'ink', label: 'Чёрный' },
      ],
    },
    { key: 'tilt', label: 'Наклон, градусы', kind: 'number', min: -15, max: 15 },
  ],
  memo: [
    {
      key: 'kind',
      label: 'Вид бланка',
      kind: 'select',
      options: [
        { value: 'memo', label: 'Записка' },
        { value: 'order', label: 'Приказ' },
        { value: 'letter', label: 'Письмо' },
      ],
    },
    { key: 'number', label: 'Номер', kind: 'text', maxLength: 40 },
    { key: 'date', label: 'Дата', kind: 'text', maxLength: 40 },
    { key: 'from', label: 'От кого', kind: 'text', maxLength: 200 },
    { key: 'to', label: 'Кому', kind: 'lines', hint: 'по одному на строке' },
    { key: 'subject', label: 'Тема', kind: 'text', maxLength: 300, wide: true },
    { key: 'signature', label: 'Подпись', kind: 'text', maxLength: 200, wide: true },
  ],
  clipping: [
    {
      key: 'kind',
      label: 'Вид документа',
      kind: 'select',
      options: [
        { value: 'newspaper', label: 'Газетная вырезка' },
        { value: 'handwritten', label: 'Рукописная запись' },
        { value: 'transcript', label: 'Расшифровка записи' },
      ],
    },
    { key: 'title', label: 'Заголовок', kind: 'text', maxLength: 300 },
    { key: 'source', label: 'Источник', kind: 'text', maxLength: 200 },
    { key: 'date', label: 'Дата', kind: 'text', maxLength: 40 },
  ],
  clipLine: [{ key: 'speaker', label: 'Говорящий', kind: 'text', maxLength: 100 }],
  table: [{ key: 'caption', label: 'Подпись к таблице', kind: 'text', maxLength: 300, wide: true }],
  docLink: [
    { key: 'code', label: 'Шифр документа', kind: 'text', maxLength: 60 },
    { key: 'note', label: 'Примечание', kind: 'text', maxLength: 300, wide: true },
  ],
  divider: [
    {
      key: 'style',
      label: 'Вид разделителя',
      kind: 'select',
      options: [
        { value: 'line', label: 'Линия' },
        { value: 'stars', label: 'Звёздочки' },
      ],
    },
  ],
  pageBreak: [{ key: 'number', label: 'Номер страницы', kind: 'text', maxLength: 20 }],
  footnote: [{ key: 'mark', label: 'Знак сноски', kind: 'text', maxLength: 8 }],
  appendix: [
    { key: 'number', label: 'Номер приложения', kind: 'text', maxLength: 20 },
    { key: 'title', label: 'Название', kind: 'text', maxLength: 200, wide: true },
  ],
}

/** Название узла редактора для подписей (тех же видов блоков, что на сервере). */
export const NODE_LABELS: Record<string, string> = {
  heading: 'Заголовок',
  paragraph: 'Абзац',
  list: 'Список',
  quote: 'Цитата',
  dossierHeader: 'Шапка досье',
  experimentLog: 'Журнал эксперимента',
  logEntry: 'Запись журнала',
  stamp: 'Штамп',
  memo: 'Бланк (записка, приказ, письмо)',
  clipping: 'Вырезка / расшифровка',
  clipLine: 'Абзац или реплика',
  table: 'Таблица',
  docLink: 'Ссылка на документ',
  divider: 'Разделитель',
  pageBreak: 'Разрыв страницы',
  footnote: 'Сноска',
  appendix: 'Приложение',
  unknownBlock: 'Неизвестный блок',
}

/** Порядок и подписи меню «Вставить блок». Ключ — вид блока сервера. */
export const INSERTABLE: { type: string; node: string; label: string; hint: string }[] = [
  { type: 'paragraph', node: 'paragraph', label: 'Абзац', hint: 'обычный текст' },
  { type: 'heading', node: 'heading', label: 'Заголовок', hint: 'раздел документа' },
  { type: 'list', node: 'list', label: 'Список', hint: 'маркеры или нумерация' },
  { type: 'quote', node: 'quote', label: 'Цитата', hint: 'с источником' },
  { type: 'dossier_header', node: 'dossierHeader', label: 'Шапка досье', hint: 'один раз на документ' },
  { type: 'experiment_log', node: 'experimentLog', label: 'Журнал эксперимента', hint: 'записи с датами и участниками' },
  { type: 'stamp', node: 'stamp', label: 'Штамп', hint: 'красный или чёрный, с наклоном' },
  { type: 'memo', node: 'memo', label: 'Бланк', hint: 'записка, приказ или письмо' },
  { type: 'clipping', node: 'clipping', label: 'Вырезка или расшифровка', hint: 'газета, рукопись, запись разговора' },
  { type: 'table', node: 'table', label: 'Таблица', hint: 'первая строка — заголовки столбцов' },
  { type: 'doc_link', node: 'docLink', label: 'Ссылка на документ', hint: 'по шифру' },
  { type: 'divider', node: 'divider', label: 'Разделитель', hint: 'линия или звёздочки' },
  { type: 'page', node: 'pageBreak', label: 'Разрыв страницы', hint: 'с номером' },
  { type: 'footnote', node: 'footnote', label: 'Сноска', hint: 'знак и текст' },
  { type: 'appendix', node: 'appendix', label: 'Приложение', hint: 'номер и название' },
]
