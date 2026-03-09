import { configureStore, combineReducers } from '@reduxjs/toolkit';
import { persistStore, persistReducer } from 'redux-persist';
import createWebStorage from 'redux-persist/lib/storage/createWebStorage';

import runReducer from './features/runSlice';

const createNoopStorage = () => {
    return {
        getItem() {
            return Promise.resolve(null);
        },
        setItem(_key: string, value: unknown) {
            return Promise.resolve(value);
        },
        removeItem() {
            return Promise.resolve();
        },
    };
};

const storage = typeof window !== 'undefined' ? createWebStorage('local') : createNoopStorage();

// Nested persist config for run slice - only persist user preferences
// Exclude: events, messages, toolHistory (contain large base64 screenshots)
const runPersistConfig = {
    key: 'run',
    storage,
    whitelist: [
        'selectedAgent',
        'researchStrategy',
    ],
};

const rootReducer = combineReducers({
    run: persistReducer(runPersistConfig, runReducer),
});

const persistConfig = {
    key: 'root',
    storage,
    blacklist: ['run'], // run has its own nested config
};

const persistedReducer = persistReducer(persistConfig, rootReducer);

export const store = configureStore({
    reducer: persistedReducer,
    middleware: (getDefaultMiddleware) =>
        getDefaultMiddleware({
            serializableCheck: {
                // Ignore all redux-persist actions (they use non-serializable callbacks)
                ignoredActions: [
                    'persist/PERSIST',
                    'persist/REHYDRATE',
                    'persist/PURGE',
                    'persist/FLUSH',
                    'persist/PAUSE',
                    'persist/REGISTER',
                ],
            },
        }),
});

export const persistor = persistStore(store);

export type RootState = ReturnType<typeof store.getState>;
export type AppDispatch = typeof store.dispatch;
