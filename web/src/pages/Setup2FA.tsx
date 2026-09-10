import { useNavigate } from 'react-router'
import { S } from '../strings'
import TotpSetup from '../components/TotpSetup'

// Cadastro obrigatório (FILEZAM_REQUIRE_2FA_ADMINS): fora do Shell, como a troca de senha forçada.
export default function Setup2FA() {
  const navigate = useNavigate()
  return (
    <div className="flex h-full items-center justify-center p-4">
      <div className="card w-full max-w-lg p-6">
        <h1 className="text-lg font-semibold">{S.totp}</h1>
        <p className="mt-2 text-sm text-amber-700 dark:text-amber-400">{S.totpRequiredBanner}</p>
        <div className="mt-4"><TotpSetup onDone={() => navigate('/b', { replace: true })} /></div>
      </div>
    </div>
  )
}
