import { Routes, Route, Navigate, useParams } from 'react-router-dom'
import { useAuth } from './auth'
import Login from './pages/Login'
import Layout from './components/Layout'
import Channels from './pages/Channels'
import ChannelEditor from './pages/ChannelEditor'
import Jobs from './pages/Jobs'
import Recipes from './pages/Recipes'
import Tools from './pages/Tools'
import Workflows from './pages/Workflows'
import WorkflowDetail from './pages/WorkflowDetail'
import Conversations from './pages/Conversations'
import ConversationDetail from './pages/ConversationDetail'

function ChannelConfigRedirect() {
  const { id } = useParams()
  return <Navigate to={`/channels/${id}`} replace />
}

function App() {
  const auth = useAuth()

  if (auth.status === 'loading') {
    return null
  }

  if (auth.status === 'unauthenticated') {
    return (
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route path="*" element={<Navigate to="/login" replace />} />
      </Routes>
    )
  }

  return (
    <Routes>
      <Route element={<Layout user={auth.user} />}>
        <Route path="/channels" element={<Channels />} />
        <Route path="/channels/:id" element={<ChannelEditor />} />
        <Route path="/channels/:id/config" element={<ChannelConfigRedirect />} />
        <Route path="/jobs" element={<Jobs />} />
        <Route path="/workflows" element={<Workflows />} />
        <Route path="/workflows/:id" element={<WorkflowDetail />} />
        <Route path="/conversations" element={<Conversations />} />
        <Route path="/conversations/:id" element={<ConversationDetail />} />
        <Route path="/recipes" element={<Recipes />} />
        <Route path="/tools" element={<Tools />} />
        <Route path="/" element={<Navigate to="/channels" replace />} />
        <Route path="*" element={<Navigate to="/channels" replace />} />
      </Route>
      <Route path="/login" element={<Navigate to="/" replace />} />
    </Routes>
  )
}

export default App
