export type Option = { id?: string; position: number; text: string; isCorrect: boolean }
export type Question = {
  id?: string
  type: 'multiple_choice'
  position: number
  prompt: string
  explanation: string
  difficulty: 'easy' | 'medium' | 'hard'
  timeLimitSeconds: number
  scoringMode: 'fixed' | 'speed'
  basePoints: number
  options: Option[]
}
export type Quiz = {
  id: string
  title: string
  description: string
  status: 'draft' | 'published' | 'archived'
  questionCount: number
  version: number
  createdAt: string
  updatedAt: string
  publishedAt: string | null
  questions: Question[]
}

export function blankQuestion(position: number): Question {
  return {
    type: 'multiple_choice',
    position,
    prompt: '',
    explanation: '',
    difficulty: 'medium',
    timeLimitSeconds: 30,
    scoringMode: 'speed',
    basePoints: 1000,
    options: [
      { position: 0, text: '', isCorrect: true },
      { position: 1, text: '', isCorrect: false },
    ],
  }
}

export function renumberQuestions(qs: Question[]): Question[] {
  return qs.map((q, i) => ({
    ...q,
    position: i,
    options: q.options.map((o, j) => ({ ...o, position: j })),
  }))
}
