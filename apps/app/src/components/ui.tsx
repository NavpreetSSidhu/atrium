// shortcut: minimal Tailwind primitives until packages/ui (shadcn/ui) exists; move them there then.
import type { ButtonHTMLAttributes, InputHTMLAttributes, ReactNode } from 'react'

export function Button({ variant = 'primary', className = '', ...props }: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: 'primary' | 'ghost' }) {
  const look =
    variant === 'primary'
      ? 'bg-indigo-600 text-white hover:bg-indigo-500 disabled:bg-indigo-300'
      : 'text-zinc-600 hover:bg-zinc-100 hover:text-zinc-900'
  return (
    <button
      className={`inline-flex h-11 items-center justify-center rounded-lg px-4 text-sm font-medium transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-indigo-600 disabled:cursor-not-allowed ${look} ${className}`}
      {...props}
    />
  )
}

export function Field({ label, hint, error, children }: { label: string; hint?: ReactNode; error?: string; children: ReactNode }) {
  return (
    <label className="block space-y-1.5">
      <span className="text-sm font-medium text-zinc-800">{label}</span>
      {children}
      {error ? <span role="alert" className="block text-sm text-red-600">{error}</span> : hint && <span className="block text-sm text-zinc-500">{hint}</span>}
    </label>
  )
}

export function Input(props: InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      className="h-11 w-full rounded-lg border border-zinc-300 bg-white px-3 text-zinc-900 placeholder:text-zinc-400 focus:border-indigo-600 focus:outline-none focus:ring-2 focus:ring-indigo-600/20"
      {...props}
    />
  )
}

export function Logo() {
  return (
    <span className="inline-flex items-center gap-2 font-semibold tracking-tight text-zinc-900">
      <span className="grid size-7 place-items-center rounded-lg bg-indigo-600 text-sm text-white">A</span>
      Atrium
    </span>
  )
}

// Centered single-card layout for sign-in and onboarding.
export function Shell({ children, wide = false }: { children: ReactNode; wide?: boolean }) {
  return (
    <div className="flex min-h-svh flex-col items-center bg-zinc-50 px-4 py-10">
      <Logo />
      <main className={`mt-8 w-full ${wide ? 'max-w-xl' : 'max-w-sm'} rounded-2xl border border-zinc-200 bg-white p-6 shadow-sm sm:p-8`}>
        {children}
      </main>
    </div>
  )
}
