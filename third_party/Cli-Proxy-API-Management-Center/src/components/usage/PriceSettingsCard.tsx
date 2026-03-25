import { useState, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { Select } from '@/components/ui/Select';
import type { ModelPrice } from '@/utils/usage';
import styles from '@/pages/UsagePage.module.scss';

export interface PriceSettingsCardProps {
  modelNames: string[];
  modelPrices: Record<string, ModelPrice>;
  onPricesChange: (prices: Record<string, ModelPrice>) => void;
}

type PriceFormValues = {
  prompt: string;
  completion: string;
  cache: string;
  fastModeMultiplier: string;
  inputOver272kPrompt: string;
  inputOver272kCompletion: string;
  inputOver272kCache: string;
};

const EMPTY_PRICE_FORM: PriceFormValues = {
  prompt: '',
  completion: '',
  cache: '',
  fastModeMultiplier: '',
  inputOver272kPrompt: '',
  inputOver272kCompletion: '',
  inputOver272kCache: ''
};

const parseNonNegativeNumber = (value: string): number | undefined => {
  const trimmed = value.trim();
  if (!trimmed) return undefined;
  const parsed = Number(trimmed);
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : undefined;
};

const priceToFormValues = (price?: ModelPrice): PriceFormValues => ({
  prompt: price?.prompt?.toString() || '',
  completion: price?.completion?.toString() || '',
  cache: price?.cache?.toString() || '',
  fastModeMultiplier: price?.fast_mode_multiplier?.toString() || '',
  inputOver272kPrompt: price?.input_over_272k?.prompt?.toString() || '',
  inputOver272kCompletion: price?.input_over_272k?.completion?.toString() || '',
  inputOver272kCache: price?.input_over_272k?.cache?.toString() || ''
});

const hasAdvancedPriceValues = (price?: ModelPrice | PriceFormValues | null) => {
  if (!price) return false;
  if ('fastModeMultiplier' in price) {
    return Boolean(
      price.fastModeMultiplier.trim() ||
        price.inputOver272kPrompt.trim() ||
        price.inputOver272kCompletion.trim() ||
        price.inputOver272kCache.trim()
    );
  }

  return Boolean(
    price.fast_mode_multiplier !== undefined ||
      price.input_over_272k?.prompt !== undefined ||
      price.input_over_272k?.completion !== undefined ||
      price.input_over_272k?.cache !== undefined
  );
};

const buildModelPrice = (fields: PriceFormValues): ModelPrice => {
  const prompt = parseNonNegativeNumber(fields.prompt) ?? 0;
  const completion = parseNonNegativeNumber(fields.completion) ?? 0;
  const cache = parseNonNegativeNumber(fields.cache) ?? prompt;
  const fastModeMultiplier = parseNonNegativeNumber(fields.fastModeMultiplier);
  const inputOver272k = {
    prompt: parseNonNegativeNumber(fields.inputOver272kPrompt),
    completion: parseNonNegativeNumber(fields.inputOver272kCompletion),
    cache: parseNonNegativeNumber(fields.inputOver272kCache)
  };

  return {
    prompt,
    completion,
    cache,
    ...(fastModeMultiplier !== undefined ? { fast_mode_multiplier: fastModeMultiplier } : {}),
    ...(Object.values(inputOver272k).some((value) => value !== undefined)
      ? {
          input_over_272k: Object.fromEntries(
            Object.entries(inputOver272k).filter(([, value]) => value !== undefined)
          )
        }
      : {})
  };
};

export function PriceSettingsCard({
  modelNames,
  modelPrices,
  onPricesChange
}: PriceSettingsCardProps) {
  const { t } = useTranslation();

  // Add form state
  const [selectedModel, setSelectedModel] = useState('');
  const [priceForm, setPriceForm] = useState<PriceFormValues>(EMPTY_PRICE_FORM);
  const [showAdvanced, setShowAdvanced] = useState(false);

  // Edit modal state
  const [editModel, setEditModel] = useState<string | null>(null);
  const [editForm, setEditForm] = useState<PriceFormValues>(EMPTY_PRICE_FORM);
  const [editShowAdvanced, setEditShowAdvanced] = useState(false);

  const updatePriceForm = (patch: Partial<PriceFormValues>) =>
    setPriceForm((prev) => ({ ...prev, ...patch }));
  const updateEditForm = (patch: Partial<PriceFormValues>) =>
    setEditForm((prev) => ({ ...prev, ...patch }));

  const handleSavePrice = () => {
    if (!selectedModel) return;
    const newPrices = { ...modelPrices, [selectedModel]: buildModelPrice(priceForm) };
    onPricesChange(newPrices);
    setSelectedModel('');
    setPriceForm(EMPTY_PRICE_FORM);
    setShowAdvanced(false);
  };

  const handleDeletePrice = (model: string) => {
    const newPrices = { ...modelPrices };
    delete newPrices[model];
    onPricesChange(newPrices);
  };

  const handleOpenEdit = (model: string) => {
    const price = modelPrices[model];
    setEditModel(model);
    const nextForm = priceToFormValues(price);
    setEditForm(nextForm);
    setEditShowAdvanced(hasAdvancedPriceValues(nextForm));
  };

  const handleSaveEdit = () => {
    if (!editModel) return;
    const newPrices = { ...modelPrices, [editModel]: buildModelPrice(editForm) };
    onPricesChange(newPrices);
    setEditModel(null);
    setEditForm(EMPTY_PRICE_FORM);
    setEditShowAdvanced(false);
  };

  const handleModelSelect = (value: string) => {
    setSelectedModel(value);
    const price = modelPrices[value];
    const nextForm = priceToFormValues(price);
    setPriceForm(nextForm);
    setShowAdvanced(hasAdvancedPriceValues(nextForm));
  };

  const options = useMemo(
    () => [
      { value: '', label: t('usage_stats.model_price_select_placeholder') },
      ...modelNames.map((name) => ({ value: name, label: name }))
    ],
    [modelNames, t]
  );

  return (
    <Card title={t('usage_stats.model_price_settings')}>
      <div className={styles.pricingSection}>
        {/* Price Form */}
        <div className={styles.priceForm}>
          <div className={styles.formRow}>
            <div className={styles.formField}>
              <label>{t('usage_stats.model_name')}</label>
              <Select
                value={selectedModel}
                options={options}
                onChange={handleModelSelect}
                placeholder={t('usage_stats.model_price_select_placeholder')}
              />
            </div>
            <div className={styles.formField}>
              <label>{t('usage_stats.model_price_prompt')} ($/1M)</label>
              <Input
                type="number"
                value={priceForm.prompt}
                onChange={(e) => updatePriceForm({ prompt: e.target.value })}
                placeholder="0.00"
                step="0.0001"
              />
            </div>
            <div className={styles.formField}>
              <label>{t('usage_stats.model_price_completion')} ($/1M)</label>
              <Input
                type="number"
                value={priceForm.completion}
                onChange={(e) => updatePriceForm({ completion: e.target.value })}
                placeholder="0.00"
                step="0.0001"
              />
            </div>
            <div className={styles.formField}>
              <label>{t('usage_stats.model_price_cache')} ($/1M)</label>
              <Input
                type="number"
                value={priceForm.cache}
                onChange={(e) => updatePriceForm({ cache: e.target.value })}
                placeholder={t('usage_stats.model_price_inherit_placeholder')}
                step="0.0001"
              />
            </div>
            <Button variant="ghost" onClick={() => setShowAdvanced((prev) => !prev)}>
              {showAdvanced
                ? t('usage_stats.model_price_hide_advanced')
                : t('usage_stats.model_price_show_advanced')}
            </Button>
            <Button variant="primary" onClick={handleSavePrice} disabled={!selectedModel}>
              {t('usage_stats.model_price_save')}
            </Button>
          </div>
          {showAdvanced && (
            <div className={styles.advancedPriceFields}>
              <div className={styles.formRow}>
                <div className={styles.formField}>
                  <label>{t('usage_stats.model_price_fast_mode_multiplier')}</label>
                  <Input
                    type="number"
                    value={priceForm.fastModeMultiplier}
                    onChange={(e) => updatePriceForm({ fastModeMultiplier: e.target.value })}
                    placeholder="1.00"
                    step="0.01"
                    hint={t('usage_stats.model_price_fast_mode_hint')}
                  />
                </div>
                <div className={styles.formField}>
                  <label>{t('usage_stats.model_price_input_over_272k_prompt')} ($/1M)</label>
                  <Input
                    type="number"
                    value={priceForm.inputOver272kPrompt}
                    onChange={(e) => updatePriceForm({ inputOver272kPrompt: e.target.value })}
                    placeholder={t('usage_stats.model_price_inherit_placeholder')}
                    step="0.0001"
                  />
                </div>
                <div className={styles.formField}>
                  <label>{t('usage_stats.model_price_input_over_272k_completion')} ($/1M)</label>
                  <Input
                    type="number"
                    value={priceForm.inputOver272kCompletion}
                    onChange={(e) => updatePriceForm({ inputOver272kCompletion: e.target.value })}
                    placeholder={t('usage_stats.model_price_inherit_placeholder')}
                    step="0.0001"
                  />
                </div>
                <div className={styles.formField}>
                  <label>{t('usage_stats.model_price_input_over_272k_cache')} ($/1M)</label>
                  <Input
                    type="number"
                    value={priceForm.inputOver272kCache}
                    onChange={(e) => updatePriceForm({ inputOver272kCache: e.target.value })}
                    placeholder={t('usage_stats.model_price_inherit_placeholder')}
                    step="0.0001"
                  />
                </div>
              </div>
            </div>
          )}
        </div>

        {/* Saved Prices List */}
        <div className={styles.pricesList}>
          <h4 className={styles.pricesTitle}>{t('usage_stats.saved_prices')}</h4>
          {Object.keys(modelPrices).length > 0 ? (
            <div className={styles.pricesGrid}>
              {Object.entries(modelPrices).map(([model, price]) => (
                <div key={model} className={styles.priceItem}>
                  <div className={styles.priceInfo}>
                    <span className={styles.priceModel}>{model}</span>
                    <div className={styles.priceMeta}>
                      <span>
                        {t('usage_stats.model_price_prompt')}: ${price.prompt.toFixed(4)}/1M
                      </span>
                      <span>
                        {t('usage_stats.model_price_completion')}: ${price.completion.toFixed(4)}/1M
                      </span>
                      <span>
                        {t('usage_stats.model_price_cache')}: ${price.cache.toFixed(4)}/1M
                      </span>
                      {price.fast_mode_multiplier !== undefined && (
                        <span>
                          {t('usage_stats.model_price_fast_mode_summary')}: x
                          {price.fast_mode_multiplier.toFixed(2)}
                        </span>
                      )}
                      {price.input_over_272k && (
                        <span>{t('usage_stats.model_price_input_over_272k_summary')}</span>
                      )}
                    </div>
                  </div>
                  <div className={styles.priceActions}>
                    <Button variant="secondary" size="sm" onClick={() => handleOpenEdit(model)}>
                      {t('common.edit')}
                    </Button>
                    <Button variant="danger" size="sm" onClick={() => handleDeletePrice(model)}>
                      {t('common.delete')}
                    </Button>
                  </div>
                </div>
              ))}
            </div>
          ) : (
            <div className={styles.hint}>{t('usage_stats.model_price_empty')}</div>
          )}
        </div>
      </div>

      {/* Edit Modal */}
      <Modal
        open={editModel !== null}
        title={editModel ?? ''}
        onClose={() => setEditModel(null)}
        footer={
          <div className={styles.priceActions}>
            <Button variant="secondary" onClick={() => setEditModel(null)}>
              {t('common.cancel')}
            </Button>
            <Button variant="primary" onClick={handleSaveEdit}>
              {t('usage_stats.model_price_save')}
            </Button>
          </div>
        }
        width={420}
      >
        <div className={styles.editModalBody}>
          <div className={styles.formField}>
            <label>{t('usage_stats.model_price_prompt')} ($/1M)</label>
            <Input
              type="number"
              value={editForm.prompt}
              onChange={(e) => updateEditForm({ prompt: e.target.value })}
              placeholder="0.00"
              step="0.0001"
            />
          </div>
          <div className={styles.formField}>
            <label>{t('usage_stats.model_price_completion')} ($/1M)</label>
            <Input
              type="number"
              value={editForm.completion}
              onChange={(e) => updateEditForm({ completion: e.target.value })}
              placeholder="0.00"
              step="0.0001"
            />
          </div>
          <div className={styles.formField}>
            <label>{t('usage_stats.model_price_cache')} ($/1M)</label>
            <Input
              type="number"
              value={editForm.cache}
              onChange={(e) => updateEditForm({ cache: e.target.value })}
              placeholder={t('usage_stats.model_price_inherit_placeholder')}
              step="0.0001"
            />
          </div>
          <Button variant="ghost" onClick={() => setEditShowAdvanced((prev) => !prev)}>
            {editShowAdvanced
              ? t('usage_stats.model_price_hide_advanced')
              : t('usage_stats.model_price_show_advanced')}
          </Button>
          {editShowAdvanced && (
            <>
              <div className={styles.formField}>
                <label>{t('usage_stats.model_price_fast_mode_multiplier')}</label>
                <Input
                  type="number"
                  value={editForm.fastModeMultiplier}
                  onChange={(e) => updateEditForm({ fastModeMultiplier: e.target.value })}
                  placeholder="1.00"
                  step="0.01"
                  hint={t('usage_stats.model_price_fast_mode_hint')}
                />
              </div>
              <div className={styles.formField}>
                <label>{t('usage_stats.model_price_input_over_272k_prompt')} ($/1M)</label>
                <Input
                  type="number"
                  value={editForm.inputOver272kPrompt}
                  onChange={(e) => updateEditForm({ inputOver272kPrompt: e.target.value })}
                  placeholder={t('usage_stats.model_price_inherit_placeholder')}
                  step="0.0001"
                />
              </div>
              <div className={styles.formField}>
                <label>{t('usage_stats.model_price_input_over_272k_completion')} ($/1M)</label>
                <Input
                  type="number"
                  value={editForm.inputOver272kCompletion}
                  onChange={(e) => updateEditForm({ inputOver272kCompletion: e.target.value })}
                  placeholder={t('usage_stats.model_price_inherit_placeholder')}
                  step="0.0001"
                />
              </div>
              <div className={styles.formField}>
                <label>{t('usage_stats.model_price_input_over_272k_cache')} ($/1M)</label>
                <Input
                  type="number"
                  value={editForm.inputOver272kCache}
                  onChange={(e) => updateEditForm({ inputOver272kCache: e.target.value })}
                  placeholder={t('usage_stats.model_price_inherit_placeholder')}
                  step="0.0001"
                />
              </div>
            </>
          )}
        </div>
      </Modal>
    </Card>
  );
}
