import { useState } from 'react'
import { ListsOverview } from './ListsOverview'
import { ListDetail } from './ListDetail'

type View = { screen: 'overview' } | { screen: 'list'; listId: string }

function App() {
  const [view, setView] = useState<View>({ screen: 'overview' })

  return view.screen === 'overview' ? (
    <ListsOverview onOpenList={(listId) => setView({ screen: 'list', listId })} />
  ) : (
    <ListDetail
      listId={view.listId}
      onBack={() => setView({ screen: 'overview' })}
      onDeleted={() => setView({ screen: 'overview' })}
    />
  )
}

export default App
