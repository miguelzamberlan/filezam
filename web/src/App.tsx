import { Navigate, Route, Routes, useLocation } from 'react-router'
import { useAuth, useConfig } from './hooks'
import { S } from './strings'
import Login from './pages/Login'
import ChangePassword from './pages/ChangePassword'
import Browser from './pages/Browser'
import Shares from './pages/Shares'
import Search from './pages/Search'
import Trash from './pages/Trash'
import Jobs from './pages/Jobs'
import AdminUsers from './pages/AdminUsers'
import AdminAudit from './pages/AdminAudit'
import PublicShare from './pages/PublicShare'
import Shell from './components/Shell'
import { DialogHost, ToastHost } from './components/dialogs'
import { ISpinner } from './components/Icons'

function Protected({ children }: { children: React.ReactNode }) {
  const { user, loading } = useAuth()
  const loc = useLocation()
  if (loading) {
    return (
      <div className="flex h-full items-center justify-center text-neutral-500">
        <ISpinner /> <span className="ml-2">{S.loading}</span>
      </div>
    )
  }
  if (!user) return <Navigate to="/login" replace state={{ from: loc.pathname }} />
  if (user.mustChangePassword && loc.pathname !== '/change-password') return <Navigate to="/change-password" replace />
  return <>{children}</>
}

function ConfigLoader() {
  useConfig()
  return null
}

export default function App() {
  return (
    <>
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route path="/s/:token/*" element={<PublicShare />} />
        <Route
          path="/change-password"
          element={
            <Protected>
              <ChangePassword />
            </Protected>
          }
        />
        <Route
          path="/*"
          element={
            <Protected>
              <ConfigLoader />
              <Shell />
            </Protected>
          }
        >
          <Route index element={<Navigate to="/b" replace />} />
          <Route path="b/*" element={<Browser />} />
          <Route path="search" element={<Search />} />
          <Route path="shares" element={<Shares />} />
          <Route path="trash" element={<Trash />} />
          <Route path="jobs" element={<Jobs />} />
          <Route path="admin/users" element={<AdminUsers />} />
          <Route path="admin/audit" element={<AdminAudit />} />
          <Route path="*" element={<Navigate to="/b" replace />} />
        </Route>
      </Routes>
      <DialogHost />
      <ToastHost />
    </>
  )
}
