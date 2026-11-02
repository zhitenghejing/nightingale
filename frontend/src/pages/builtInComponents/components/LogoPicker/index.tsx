import React, { useContext, useEffect, useRef, useState } from 'react';
import { Alert, Button, Modal, Spin, message } from 'antd';
import { PlusOutlined } from '@ant-design/icons';
import { useTranslation } from 'react-i18next';
import { CommonStateContext } from '@/App';
import { errorText } from '@/pages/collection/errors';
import { Component } from '../../services';
import { addTemplateIcon, getTemplateIcons, TemplateIcon } from '../../catalog';
import './style.less';

interface Props {
  components: Component[];
  children: React.ReactNode;
  onSelect: (logoURL: string) => void;
  libraryEnabled?: boolean;
}

export default function LogoPicker({ components, children, onSelect, libraryEnabled = true }: Props) {
  const { t } = useTranslation('builtInComponents');
  const { profile } = useContext(CommonStateContext);
  const [visible, setVisible] = useState(false);
  const [icons, setIcons] = useState<TemplateIcon[]>([]);
  const [loading, setLoading] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [error, setError] = useState('');
  const input = useRef<HTMLInputElement>(null);
  const load = () => {
    setLoading(true);
    setError('');
    return getTemplateIcons()
      .then(setIcons)
      .catch((e) => setError(errorText(e, t('logo_library_failed'))))
      .finally(() => setLoading(false));
  };
  useEffect(() => {
    if (visible && libraryEnabled) load();
  }, [visible, libraryEnabled]);
  const select = (url: string) => {
    setVisible(false);
    onSelect(url);
  };
  const upload = async (file?: File) => {
    if (!file || uploading) return;
    if (!file.size || file.size > 2 * 1024 * 1024 || !['image/png', 'image/jpeg', 'image/svg+xml', 'image/webp'].includes(file.type)) {
      message.error(t('logo_upload_hint'));
      return;
    }
    setUploading(true);
    try {
      const data = await new Promise<string>((resolve, reject) => {
        const reader = new FileReader();
        reader.onload = () => resolve(String(reader.result));
        reader.onerror = () => reject(new Error(t('logo_upload_failed')));
        reader.readAsDataURL(file);
      });
      const icon = await addTemplateIcon(file.name.slice(0, 128), data);
      setIcons((current) => (current.some((item) => item.id === icon.id) ? current : [...current, icon]));
      message.success(t('logo_upload_success'));
    } catch (e) {
      message.error(errorText(e, t('logo_upload_failed')));
    } finally {
      setUploading(false);
    }
  };

  return (
    <>
      <Modal width={700} bodyStyle={{ maxHeight: '70vh', overflowY: 'auto' }} visible={visible} title={t('logo_picker_title')} onCancel={() => setVisible(false)} footer={null}>
        <div className='n9e-logo-picker-content'>
          {components
            .filter((item) => !!item.logo)
            .map((item) => (
              <button key={item.id} type='button' className='n9e-logo-picker-item' title={item.ident} aria-label={item.ident} onClick={() => select(item.logo)}>
                <img src={item.logo} alt='' />
              </button>
            ))}
        </div>
        {libraryEnabled && (
          <div className='n9e-logo-picker-library'>
            <div className='mb-2'>{t('logo_library_title')}</div>
            {loading && <Spin size='small' />}
            {error && (
              <Alert
                type='error'
                message={error}
                action={
                  <Button size='small' onClick={load}>
                    {t('logo_retry')}
                  </Button>
                }
              />
            )}
            <div className='n9e-logo-picker-content'>
              {icons.map((icon) => (
                <button key={icon.id} type='button' className='n9e-logo-picker-item' title={icon.name} aria-label={icon.name} onClick={() => select(icon.url)}>
                  <img src={icon.url} alt='' />
                </button>
              ))}
            </div>
            {profile?.admin && (
              <>
                <input
                  ref={input}
                  type='file'
                  hidden
                  aria-label={t('logo_add_material')}
                  accept='image/png,image/jpeg,image/svg+xml,image/webp'
                  onChange={(event) => {
                    const file = event.target.files?.[0];
                    event.target.value = '';
                    upload(file);
                  }}
                />
                <Button
                  className='n9e-logo-picker-add'
                  aria-label={t('logo_add_material')}
                  type='dashed'
                  block
                  icon={<PlusOutlined />}
                  loading={uploading}
                  disabled={loading}
                  onClick={() => input.current?.click()}
                >
                  {t('logo_add_material')}
                </Button>
                <div className='n9e-logo-picker-hint'>{t('logo_upload_hint')}</div>
              </>
            )}
          </div>
        )}
      </Modal>
      <div className='n9e-logo-picker-trigger' onClick={() => setVisible(true)}>
        {children}
      </div>
    </>
  );
}
